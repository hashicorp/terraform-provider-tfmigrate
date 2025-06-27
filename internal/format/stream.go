// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package format

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

const (
	defaultColumns uint = 78
)

var _ io.Writer = (*Stream)(nil)

// Stream encapsulates a single output stream (for example stdout or stderr) and
// provides some helper functions for writing.
//
// TODO: Extend this with line wrapping.
type Stream struct {
	*os.File
}

func (stream *Stream) Columns() uint {
	width, _, err := term.GetSize(int(stream.File.Fd())) //nolint:staticcheck
	if err != nil {
		return defaultColumns
	}
	return uint(width)
}

func (stream *Stream) Print(args ...any) {
	fmt.Fprint(stream, args...)
}
