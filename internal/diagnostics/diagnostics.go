// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package diagnostics

import (
	"fmt"

	"github.com/hashicorp/go-slug/sourcebundle"
	tfe "github.com/hashicorp/go-tfe"
	"github.com/hashicorp/hcl/v2"

	"github.com/hashicorp/terraform/tfdiags"

	sev "github.com/hashicorp/terraform/tfdiags"
	terraformcore "terraform-provider-tfmigrate/internal/terraform/rpcapi/terraform1"
)

type Diagnostics []Diagnostic

func (diags Diagnostics) Append(extra ...any) Diagnostics {
	for _, diag := range extra {
		switch diag := diag.(type) {
		case Diagnostic:
			diags = append(diags, diag)
		case Diagnostics:
			diags = append(diags, diag...)
		case *hcl.Diagnostic:
			diags = append(diags, &hclDiagnostic{diag})
		case hcl.Diagnostics:
			for _, diag := range diag {
				diags = append(diags, &hclDiagnostic{diag})
			}
		case sourcebundle.Diagnostic:
			diags = append(diags, &sourcebundleDiagnostic{diag})
		case sourcebundle.Diagnostics:
			for _, diag := range diag {
				diags = append(diags, &sourcebundleDiagnostic{diag})
			}
		case *terraformcore.Diagnostic:
			diags = append(diags, &protoDiagnostic{diag})
		case []*terraformcore.Diagnostic:
			for _, diag := range diag {
				diags = append(diags, &protoDiagnostic{diag})
			}
		case tfdiags.Diagnostic:
			diags = append(diags, tfDiagToDiagnostic(diag))
		case tfdiags.Diagnostics:
			for _, diag := range diag {
				diags = append(diags, tfDiagToDiagnostic(diag))
			}
		case tfe.StackDiagnostic:
			diags = append(diags, &tfeDiagnostic{diag})
		case []*tfe.StackDiagnostic:
			for _, d := range diag {
				diags = append(diags, &tfeDiagnostic{*d})
			}
		default:
			panic(fmt.Errorf("unrecognized diagnostic: %T", diag))
		}
	}
	return diags
}

func (diags Diagnostics) HasErrors() bool {
	for _, diag := range diags {
		if diag.Severity() == sev.Error {
			return true
		}
	}
	return false
}

func ErrToDiagnostics(err error, summary string) Diagnostics {
	return Diagnostics{
		Sourceless(
			sev.Error,
			summary,
			err.Error(),
		),
	}
}
