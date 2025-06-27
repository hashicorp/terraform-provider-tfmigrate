// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package diagnostics

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"

	terraformcore "terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1"
)

var _ Diagnostic = (*protoDiagnostic)(nil)

type protoDiagnostic struct {
	diag *terraformcore.Diagnostic
}

func (d *protoDiagnostic) Severity() Severity {
	switch d.diag.Severity {
	case terraformcore.Diagnostic_ERROR:
		return Error
	case terraformcore.Diagnostic_WARNING:
		return Warning
	default:
		panic(fmt.Sprintf("invalid severity: %d", d.diag.Severity))
	}
}

func (d *protoDiagnostic) Summary() string {
	return d.diag.Summary
}

func (d *protoDiagnostic) Detail() string {
	return d.diag.Detail
}

func (d *protoDiagnostic) Subject() *hcl.Range {
	if d.diag.Subject == nil {
		return nil
	}

	return &hcl.Range{
		Filename: d.diag.Subject.SourceAddr,
		Start: hcl.Pos{
			Line:   int(d.diag.Subject.Start.Line),
			Column: int(d.diag.Subject.Start.Column),
			Byte:   int(d.diag.Subject.Start.Byte),
		},
		End: hcl.Pos{
			Line:   int(d.diag.Subject.End.Line),
			Column: int(d.diag.Subject.End.Column),
			Byte:   int(d.diag.Subject.End.Byte),
		},
	}
}

func (d *protoDiagnostic) Context() *hcl.Range {
	if d.diag.Context == nil {
		return nil
	}

	return &hcl.Range{
		Filename: d.diag.Context.SourceAddr,
		Start: hcl.Pos{
			Line:   int(d.diag.Context.Start.Line),
			Column: int(d.diag.Context.Start.Column),
			Byte:   int(d.diag.Context.Start.Byte),
		},
		End: hcl.Pos{
			Line:   int(d.diag.Context.End.Line),
			Column: int(d.diag.Context.End.Column),
			Byte:   int(d.diag.Context.End.Byte),
		},
	}
}

func (d *protoDiagnostic) Extra() interface{} {
	return nil
}
