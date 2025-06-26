package test_diagnostics

import (
	"fmt"
	"strings"
	"testing"

	"terraform-provider-tfmigrate/internal/diagnostics"

	sev "github.com/hashicorp/terraform/tfdiags"
)

type Diagnostic struct {
	Matches Matcher
	Message string
}

type Matcher func(diag diagnostics.Diagnostic) bool

func Matches(severity sev.Severity, summary, detail string) Diagnostic {
	return Diagnostic{
		Matches: func(diag diagnostics.Diagnostic) bool {
			return diag.Severity() == severity && diag.Summary() == summary && diag.Detail() == detail
		},
		Message: fmt.Sprintf("no diagnostic with severity %q, detail %q, and summary %q found", strings.ToLower(severity.String()), detail, summary),
	}
}

func Contains(t *testing.T, diags diagnostics.Diagnostics, tests ...Diagnostic) {
	t.Helper()

tests:
	for _, test := range tests {
		for _, diag := range diags {
			if test.Matches(diag) {
				continue tests
			}
		}
		t.Error(test.Message)
	}
}

func ContainsExact(t *testing.T, diags diagnostics.Diagnostics, tests ...Diagnostic) {
	t.Helper()

	unmatchedDiags := make(diagnostics.Diagnostics, len(diags))
	for ix, diag := range diags {
		unmatchedDiags[ix] = diag
	}

tests:
	for _, test := range tests {
		for ix, diag := range unmatchedDiags {
			if test.Matches(diag) {
				unmatchedDiags = append(unmatchedDiags[:ix], unmatchedDiags[ix+1:]...)
				continue tests
			}
		}
		t.Error(test.Message)
	}

	for _, diag := range unmatchedDiags {
		t.Errorf("extra diagnostic with severity %q, detail %q, and summary %q found", strings.ToLower(diag.Severity().String()), diag.Detail(), diag.Summary())
	}
}

func ContainsExactOrdered(t *testing.T, diags diagnostics.Diagnostics, tests ...Diagnostic) {
	t.Helper()

	length := max(len(diags), len(tests))
	for ix := 0; ix < length; ix++ {
		if ix >= len(diags) {
			t.Errorf("%d: %s", ix, tests[ix].Message)
			continue
		}

		if ix >= len(tests) {
			t.Errorf("%d: extra diagnostic with severity %q, detail %q, and summary %q found", ix, strings.ToLower(diags[ix].Severity().String()), diags[ix].Detail(), diags[ix].Summary())
			continue
		}

		if !tests[ix].Matches(diags[ix]) {
			t.Errorf("%d: %s\n   diagnostic with severity %q, detail %q, and summary %q found", ix, tests[ix].Message, strings.ToLower(diags[ix].Severity().String()), diags[ix].Detail(), diags[ix].Summary())
		}
	}
}

func max(i, j int) int {
	if i > j {
		return i
	}
	return j
}
