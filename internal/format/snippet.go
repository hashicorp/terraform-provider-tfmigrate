// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package format

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hcled"

	configs "terraform-provider-tfmigrate/internal/configs"
	"terraform-provider-tfmigrate/internal/diagnostics"
)

// DiagnosticSnippet represents source code information for a given
// hcl.SourceRange input.
//
// The structure matches part of the implementation within Terraform:
//   - https://github.com/hashicorp/terraform/blob/d9d88b92439b9047570bcfa5fc1e90cf71aec60d/command/views/json/diagnostic.go#L67
//
// For now, the implementation of our snippet is simpler than the Terraform
// equivalent so some of the fields haven't been carried over.
type DiagnosticSnippet struct {
	Context              *string
	Code                 string
	StartLine            int
	HighlightStartOffset int
	HighlightEndOffset   int
}

func Snippet(diag diagnostics.Diagnostic, files *configs.Files) *DiagnosticSnippet {
	var snippet *DiagnosticSnippet
	if subj := diag.Subject(); subj != nil {
		subject, context := normalizeRanges(subj, diag.Context())

		// If we can't get the file, then we just won't add any extra
		// diagnostics.
		file, _ := files.Get(subject.Filename)
		if file != nil {
			snippet = &DiagnosticSnippet{
				StartLine: context.Start.Line,
			}

			contextString := hcled.ContextString(file.Config, subject.Start.Byte-1)
			if len(contextString) > 0 {
				snippet.Context = &contextString
			}

			code, codeStartByte := file.CodeString(context)
			snippet.Code = code

			// start and end are the start and end of the subject relative to
			// the context.
			start := subject.Start.Byte - codeStartByte
			end := start + (subject.End.Byte - subject.Start.Byte)

			snippet.HighlightStartOffset = normalizeByte(start, -1, code)
			snippet.HighlightEndOffset = normalizeByte(end, start, code)
		}
	}
	return snippet
}

// normalizeRanges makes sure the provided subject (s) and context (c) contain acceptable values
func normalizeRanges(s *hcl.Range, c *hcl.Range) (hcl.Range, hcl.Range) {
	subject := *s
	if subject.Empty() {
		subject.End.Byte++
		subject.End.Column++
	}

	var context hcl.Range
	if c == nil {
		context = subject
	} else {
		context = *c
		if context.Empty() {
			context.End.Byte++
			context.End.Column++
		}
	}

	context = hcl.RangeOver(context, subject)
	return subject, context
}

// normalizeByte makes sure the value is above or equal to floor, and between 0 and len(snippet)
// floor will be ignored if it is less than zero.
func normalizeByte(b, floor int, snippet string) int {
	if floor >= 0 {
		if b < floor {
			b = floor + 1
		}
	}

	if b < 0 {
		return 0
	}

	if b > len(snippet) {
		return len(snippet)
	}

	return b
}
