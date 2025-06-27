// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package diagnostics

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
)

var _ Diagnostic = (*hclDiagnostic)(nil)

type hclDiagnostic struct {
	diag *hcl.Diagnostic
}

func (d *hclDiagnostic) Severity() Severity {
	switch d.diag.Severity {
	case hcl.DiagError:
		return Error
	case hcl.DiagWarning:
		return Warning
	default:
		panic(fmt.Sprintf("invalid severity: %d", d.diag.Severity))
	}
}

func (d *hclDiagnostic) Summary() string {
	return d.diag.Summary
}

func (d *hclDiagnostic) Detail() string {
	return d.diag.Detail
}

func (d *hclDiagnostic) Subject() *hcl.Range {
	return d.diag.Subject
}

func (d *hclDiagnostic) Context() *hcl.Range {
	return d.diag.Context
}

func (d *hclDiagnostic) Extra() interface{} {
	return d.diag.Extra
}
