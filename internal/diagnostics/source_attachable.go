// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package diagnostics

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
)

// AttachSource attaches a source range to all diagnostics that match the
// sourceAttachableDiagnostic type.
func AttachSource(diagnostics Diagnostics, source *hcl.Range) Diagnostics {
	var result Diagnostics
	for _, diag := range diagnostics {
		if diag, ok := diag.(*sourceAttachableDiagnostic); ok && diag.source == nil {
			diag.source = source
		}
		result = append(result, diag)
	}
	return result
}

type sourceAttachableDiagnostic struct {
	sourceless *sourcelessDiagnostic
	source     *hcl.Range
}

// SourceAttachable returns a Diagnostic that can be attached to a source range
// later by the AttachSource.
func SourceAttachable(severity Severity, summary, detail string, args ...any) Diagnostic {
	return &sourceAttachableDiagnostic{
		sourceless: &sourcelessDiagnostic{
			severity: severity,
			summary:  summary,
			detail:   fmt.Sprintf(detail, args...),
		},
	}
}

func (s *sourceAttachableDiagnostic) Severity() Severity {
	return s.sourceless.Severity()
}

func (s *sourceAttachableDiagnostic) Summary() string {
	return s.sourceless.Summary()
}

func (s *sourceAttachableDiagnostic) Detail() string {
	return s.sourceless.Detail()
}

func (s *sourceAttachableDiagnostic) Subject() *hcl.Range {
	return s.source
}

func (s *sourceAttachableDiagnostic) Context() *hcl.Range {
	return nil
}

func (s *sourceAttachableDiagnostic) Extra() interface{} {
	return nil
}
