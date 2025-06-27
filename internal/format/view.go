// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package format

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"github.com/mitchellh/colorstring"
	"github.com/mitchellh/go-wordwrap"

	config "terraform-provider-tfmigrate/internal/configs"
	"terraform-provider-tfmigrate/internal/diagnostics"
)

type View interface {
	Diagnostics(diags diagnostics.Diagnostics)
}

type human struct {
	files    *config.Files
	streams  *Streams
	colorize *colorstring.Colorize
}

func (h *human) Diagnostics(diags diagnostics.Diagnostics) {
	width := h.streams.Stderr.Columns()

	for _, diag := range diags {
		if h.colorize.Disable {
			h.plainDiagnostic(diag, width)
			continue
		}
		h.diagnostic(diag, width)
	}
}

func (h *human) diagnostic(diag diagnostics.Diagnostic, width uint) {
	var builder bytes.Buffer

	var leftRuleStart, leftRuleLine, leftRuleEnd string
	var leftRuleWidth uint

	switch diag.Severity() {
	case diagnostics.Error:
		leftRuleStart = h.colorize.Color("[red]╷[reset]\n")
		leftRuleLine = h.colorize.Color("[red]│[reset] ")
		leftRuleEnd = h.colorize.Color("[red]╵[reset]\n")
		leftRuleWidth = 2

		builder.WriteString(h.colorize.Color("[red][bold]Error: [reset]"))
	case diagnostics.Warning:
		leftRuleStart = h.colorize.Color("[yellow]╷[reset]\n")
		leftRuleLine = h.colorize.Color("[yellow]│[reset] ")
		leftRuleEnd = h.colorize.Color("[yellow]╵[reset]\n")
		leftRuleWidth = 2

		builder.WriteString(h.colorize.Color("[yellow][bold]Warning: [reset]"))
	default:
		leftRuleStart = "\n"
		builder.WriteString(h.colorize.Color("[reset]"))
	}

	fmt.Fprintf(&builder, h.colorize.Color("[bold]%s[reset]\n\n"), diag.Summary())

	if snippet := Snippet(diag, h.files); snippet != nil {
		subject := diag.Subject()

		// This is a bit more simplified version of the same code within
		// Terraform proper.

		var contextString string
		if snippet.Context != nil {
			contextString = fmt.Sprintf(", in %s", *snippet.Context)
		}
		fmt.Fprintf(&builder, "  on %s line %d%s:\n", subject.Filename, subject.Start.Line, contextString)

		code, start, end := snippet.Code, snippet.HighlightStartOffset, snippet.HighlightEndOffset
		before, highlight, after := code[0:start], code[start:end], code[end:]
		code = fmt.Sprintf(h.colorize.Color("%s[underline]%s[reset]%s"), before, highlight, after)

		lines := strings.Split(code, "\n")
		for ix, line := range lines {
			fmt.Fprintf(&builder, "%4d: %s\n", snippet.StartLine+ix, line)
		}
		fmt.Fprintf(&builder, "\n")

	} else if subject := diag.Subject(); subject != nil {
		// Then for some reason we couldn't load the snippet above.
		fmt.Fprintf(&builder, "  on %s line %d:\n  (source code not available)\n", subject.Filename, subject.Start.Line)
	}

	if detail := diag.Detail(); len(detail) > 0 {
		width := width - leftRuleWidth // remember offset for the left rule
		fmt.Fprintf(&builder, "%s\n", wordwrap.WrapString(detail, width))
	}

	// Do some post-processing, adding the leftRuleLine bit at the start of
	// every line so the diagnostic will stand out a bit.

	var buffer strings.Builder
	buffer.WriteString(leftRuleStart)
	for sc := bufio.NewScanner(&builder); sc.Scan(); {
		prefix, line := leftRuleLine, sc.Text()
		if len(line) == 0 {
			// don't print the trailing space if the line empty.
			prefix = strings.TrimSpace(prefix)
		}
		buffer.WriteString(prefix)
		buffer.WriteString(line)
		buffer.WriteString("\n")
	}
	buffer.WriteString(leftRuleEnd)

	h.streams.Eprint(buffer.String())
}

func (h *human) plainDiagnostic(diag diagnostics.Diagnostic, width uint) {
	var builder strings.Builder

	switch diag.Severity() {
	case diagnostics.Error:
		builder.WriteString("\nError: ")
	case diagnostics.Warning:
		builder.WriteString("\nWarning: ")
	default:
		builder.WriteString("\n")
	}

	fmt.Fprintf(&builder, "%s\n\n", diag.Summary())

	if snippet := Snippet(diag, h.files); snippet != nil {
		subject := diag.Subject()

		// This is a bit more simplified version of the same code within
		// Terraform proper.

		var contextString string
		if snippet.Context != nil {
			contextString = fmt.Sprintf(", in %s", *snippet.Context)
		}
		fmt.Fprintf(&builder, "  on %s line %d%s:\n", subject.Filename, subject.Start.Line, contextString)

		lines := strings.Split(snippet.Code, "\n")
		for ix, line := range lines {
			fmt.Fprintf(&builder, "%4d: %s\n", snippet.StartLine+ix, line)
		}
		fmt.Fprintf(&builder, "\n")

	} else if subject := diag.Subject(); subject != nil {
		// Then for some reason we couldn't load the snippet above.
		fmt.Fprintf(&builder, "  on %s line %d:\n  (source code not available)\n", subject.Filename, subject.Start.Line)
	}

	if detail := diag.Detail(); len(detail) > 0 {
		fmt.Fprintf(&builder, "%s\n", wordwrap.WrapString(detail, width))
	}

	// We've word-wrapped the individual lines above, so we're not doing
	// that again here.
	h.streams.Eprint(builder.String())
}
