// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package diagnostics

import (
	"fmt"

	"github.com/hashicorp/go-slug/sourcebundle"
	"github.com/hashicorp/hcl/v2"
	sev "github.com/hashicorp/terraform/tfdiags"
)

var _ Diagnostic = (*sourcebundleDiagnostic)(nil)

type sourcebundleDiagnostic struct {
	diag sourcebundle.Diagnostic
}

func (d *sourcebundleDiagnostic) Severity() sev.Severity {
	switch d.diag.Severity() {
	case sourcebundle.DiagError:
		return sev.Error
	case sourcebundle.DiagWarning:
		return sev.Warning
	default:
		panic(fmt.Sprintf("unrecognized sourebundle severity: %d", d.diag.Severity()))
	}
}

func (d *sourcebundleDiagnostic) Summary() string {
	return d.diag.Description().Summary
}

func (d *sourcebundleDiagnostic) Detail() string {
	return d.diag.Description().Detail
}

func (d *sourcebundleDiagnostic) Subject() *hcl.Range {
	if d.diag.Source().Subject == nil {
		return nil
	}

	return &hcl.Range{
		Filename: d.diag.Source().Subject.Filename,
		Start: hcl.Pos{
			Line:   d.diag.Source().Subject.Start.Line,
			Column: d.diag.Source().Subject.Start.Column,
			Byte:   d.diag.Source().Subject.Start.Byte,
		},
		End: hcl.Pos{
			Line:   d.diag.Source().Subject.End.Line,
			Column: d.diag.Source().Subject.End.Column,
			Byte:   d.diag.Source().Subject.End.Byte,
		},
	}
}

func (d *sourcebundleDiagnostic) Context() *hcl.Range {
	if d.diag.Source().Context == nil {
		return nil
	}

	return &hcl.Range{
		Filename: d.diag.Source().Context.Filename,
		Start: hcl.Pos{
			Line:   d.diag.Source().Context.Start.Line,
			Column: d.diag.Source().Context.Start.Column,
			Byte:   d.diag.Source().Context.Start.Byte,
		},
		End: hcl.Pos{
			Line:   d.diag.Source().Context.End.Line,
			Column: d.diag.Source().Context.End.Column,
			Byte:   d.diag.Source().Context.End.Byte,
		},
	}
}

func (d *sourcebundleDiagnostic) Extra() interface{} {
	return d.diag.ExtraInfo()
}
