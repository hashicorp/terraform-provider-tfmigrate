// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package diagnostics

import (
	"github.com/hashicorp/hcl/v2"
	sev "github.com/hashicorp/terraform/tfdiags"
)

type Diagnostic interface {
	Severity() sev.Severity

	Summary() string
	Detail() string

	Subject() *hcl.Range
	Context() *hcl.Range

	Extra() interface{}
}
