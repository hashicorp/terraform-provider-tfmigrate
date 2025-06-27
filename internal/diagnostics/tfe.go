// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package diagnostics

import (
	tfe "github.com/hashicorp/go-tfe"
	"github.com/hashicorp/hcl/v2"
)

var _ Diagnostic = (*tfeDiagnostic)(nil)

type tfeDiagnostic struct {
	diag tfe.StackDiagnostic
}

func (d *tfeDiagnostic) Severity() Severity {
	switch d.diag.Severity {
	case "warning":
		return Warning
	case "error":
		return Error
	default:
		return Error
	}
}

func (d *tfeDiagnostic) Summary() string {
	return d.diag.Summary
}

func (d *tfeDiagnostic) Detail() string {
	return d.diag.Detail
}

func (d *tfeDiagnostic) Subject() *hcl.Range {
	r := d.diag.Range
	if r == nil {
		return nil
	}

	return &hcl.Range{
		Filename: r.Filename,
		Start: hcl.Pos{
			Line:   r.Start.Line,
			Column: r.Start.Column,
			Byte:   r.Start.Byte,
		},
		End: hcl.Pos{
			Line:   r.End.Line,
			Column: r.End.Column,
			Byte:   r.End.Byte,
		},
	}
}

// We currently don't implement extra or context for external diagnostics.
func (d *tfeDiagnostic) Extra() interface{} {
	return nil
}
func (d *tfeDiagnostic) Context() *hcl.Range {

	return nil
}
