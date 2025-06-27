// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package configs

import (
	"bufio"
	"os"
	"path"
	"strings"

	"github.com/hashicorp/go-slug/sourceaddrs"
	"github.com/hashicorp/go-slug/sourcebundle"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/json"

	"terraform-provider-tfmigrate/internal/diagnostics"
)

// Files contains our cache of opened HCL files
// We maintain a cache so that our diagnostic processing can easily retrieve
// files.
type Files struct {
	cache  map[string]*File
	bundle *sourcebundle.Bundle
}

func NewFilesForBundle(bundle *sourcebundle.Bundle) *Files {
	return &Files{
		cache:  make(map[string]*File),
		bundle: bundle,
	}
}

// File wraps a hcl.File containing some Terraform configuration
// It embeds the raw bytes of the original file to help with the processing of
// diagnostic source ranges.
type File struct {
	filepath string
	raw      []byte

	Config *hcl.File
}

// Get retrieves an existing or opens a new configuration file, and pre-parses
// it as an HCL file.
func (files Files) Get(filepath string) (*File, diagnostics.Diagnostics) {
	if file, exists := files.cache[filepath]; exists {
		return file, nil
	}

	var diags diagnostics.Diagnostics

	// The path we read from may differ from the cache key, if `filepath` is a
	// source address.
	readpath := filepath

	// If we have a bundle, speculatively attempt to parse the diagnostic
	// filename as a source address. Terraform diagnostics will use the
	// filename field to pass the source address of the related file, and if
	// we're able to parse and find it, we look up the corresponding local
	// path.
	if files.bundle != nil {
		if source, err := sourceaddrs.ParseFinalSource(filepath); err == nil {
			if localpath, err := files.bundle.LocalPathForSource(source); err == nil {
				readpath = localpath
			}
		}
	}

	raw, err := os.ReadFile(readpath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}

		diags = append(diags, diagnostics.SourceAttachable(diagnostics.Error, "Failed to open configuration file", "%s", err.Error()))
		return nil, diags
	}

	// We support JSON files.
	if path.Ext(filepath) == ".json" {
		config, configDiags := json.Parse(raw, filepath)
		diags = diags.Append(configDiags)
		if config != nil {
			file := &File{
				filepath: filepath,
				raw:      raw,
				Config:   config,
			}
			files.cache[filepath] = file
			return file, diags
		}
		return nil, diags
	}

	// Otherwise try and parse it as a .hcl file.
	config, configDiags := hclsyntax.ParseConfig(raw, filepath, hcl.InitialPos)
	diags = diags.Append(configDiags)
	if config != nil {
		file := &File{
			filepath: filepath,
			raw:      raw,
			Config:   config,
		}
		files.cache[filepath] = file
		return file, diags
	}
	return nil, diags
}

// CodeString returns a string that contains the code for the provided range.
func (file *File) CodeString(targetRange hcl.Range) (string, int) {
	var code strings.Builder
	var start int

	scanner := hcl.NewRangeScanner(file.raw, file.filepath, bufio.ScanLines)
	for scanner.Scan() {
		lineRange := scanner.Range()
		if lineRange.Overlaps(targetRange) {
			if start == 0 && code.Len() == 0 {
				start = lineRange.Start.Byte
			}
			code.Write(lineRange.SliceBytes(file.raw))
			code.WriteRune('\n')
		}
	}

	return strings.TrimSuffix(code.String(), "\n"), start
}
