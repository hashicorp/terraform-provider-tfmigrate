// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package diagnostics

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
)

func tfDiagToDiagnostic(diag TFDiagnostic) Diagnostic {
	var severity Severity
	switch diag.Severity() {
	case Error:
		severity = Error
	case Warning:
		severity = Warning
	}
	return Sourceless(
		severity,
		diag.Description().Summary,
		"%s", diag.Description().Detail,
	)
}

type TFDiagnostics []TFDiagnostic
type TFDiagnostic interface {
	Severity() Severity
	Description() Description
	Source() Source

	// FromExpr returns the expression-related context for the diagnostic, if
	// available. Returns nil if the diagnostic is not related to an
	// expression evaluation.
	FromExpr() *FromExpr

	// ExtraInfo returns the raw extra information value. This is a low-level
	// API which requires some work on the part of the caller to properly
	// access associated information, so in most cases it'll be more convenient
	// to use the package-level ExtraInfo function to try to unpack a particular
	// specialized interface from this value.
	ExtraInfo() interface{}
}

type Description struct {
	Address string
	Summary string
	Detail  string
}

type Source struct {
	Subject *SourceRange
	Context *SourceRange
}

type FromExpr struct {
	Expression  hcl.Expression
	EvalContext *hcl.EvalContext
}

type SourceRange struct {
	Filename   string
	Start, End SourcePos
}

type SourcePos struct {
	Line, Column, Byte int
}

// StartString returns a string representation of the start of the range,
// including the filename and the line and column numbers.
func (r SourceRange) StartString() string {
	filename := r.Filename

	// We'll try to relative-ize our filename here so it's less verbose
	// in the common case of being in the current working directory. If not,
	// we'll just show the full path.
	wd, err := os.Getwd()
	if err == nil {
		relFn, err := filepath.Rel(wd, filename)
		if err == nil {
			filename = relFn
		}
	}

	return fmt.Sprintf("%s:%d,%d", filename, r.Start.Line, r.Start.Column)
}
