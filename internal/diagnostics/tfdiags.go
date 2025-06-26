// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package diagnostics

import (
	"github.com/hashicorp/terraform/tfdiags"
	sev "github.com/hashicorp/terraform/tfdiags"
)

func tfDiagToDiagnostic(diag tfdiags.Diagnostic) Diagnostic {
	var severity sev.Severity
	switch diag.Severity() {
	case tfdiags.Error:
		severity = sev.Error
	case tfdiags.Warning:
		severity = sev.Warning
	}
	return Sourceless(
		severity,
		diag.Description().Summary,
		diag.Description().Detail,
	)
}

func TfDiagsToDiagnostics(diags tfdiags.Diagnostics) Diagnostics {
	var result Diagnostics
	for _, diag := range diags {
		result = append(result, tfDiagToDiagnostic(diag))
	}
	return result
}
