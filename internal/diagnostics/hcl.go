// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package diagnostics

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	sev "github.com/hashicorp/terraform/tfdiags"
)

var _ Diagnostic = (*hclDiagnostic)(nil)

type hclDiagnostic struct {
	diag *hcl.Diagnostic
}

func (d *hclDiagnostic) Severity() sev.Severity {
	switch d.diag.Severity {
	case hcl.DiagError:
		return sev.Error
	case hcl.DiagWarning:
		return sev.Warning
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
