// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package diagnostics

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
)

type Diagnostic interface {
	Severity() Severity

	Summary() string
	Detail() string

	Subject() *hcl.Range
	Context() *hcl.Range

	Extra() interface{}
}

type Severity rune

//go:generate go run golang.org/x/tools/cmd/stringer -type=Severity

const (
	Error   Severity = 'E'
	Warning Severity = 'W'
)

// ToHCL converts a Severity to the equivalent HCL diagnostic severity.
func (s Severity) ToHCL() hcl.DiagnosticSeverity {
	switch s {
	case Warning:
		return hcl.DiagWarning
	case Error:
		return hcl.DiagError
	default:
		// The above should always be exhaustive for all of the valid
		// Severity values in this package.
		panic(fmt.Sprintf("unknown diagnostic severity %s", s))
	}
}
