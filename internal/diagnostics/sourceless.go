// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package diagnostics

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
)

var _ Diagnostic = (*sourcelessDiagnostic)(nil)

func Sourceless(severity Severity, summary, detail string, args ...any) Diagnostic {
	return &sourcelessDiagnostic{
		severity: severity,
		summary:  summary,
		detail:   fmt.Sprintf(detail, args...),
	}
}

type sourcelessDiagnostic struct {
	severity Severity
	summary  string
	detail   string
}

func (d *sourcelessDiagnostic) Severity() Severity {
	return d.severity
}

func (d *sourcelessDiagnostic) Summary() string {
	return d.summary
}

func (d *sourcelessDiagnostic) Detail() string {
	return d.detail
}

func (d *sourcelessDiagnostic) Subject() *hcl.Range {
	return nil
}

func (d *sourcelessDiagnostic) Context() *hcl.Range {
	return nil
}

func (d *sourcelessDiagnostic) Extra() interface{} {
	return nil
}
