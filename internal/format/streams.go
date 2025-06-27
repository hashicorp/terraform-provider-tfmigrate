// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0.

package format

// Streams encapsulates our Stdout and Stderr output streams, wrapped with some
// helper functions.
type Streams struct {
	Stdout *Stream
	Stderr *Stream
}

func (streams *Streams) Eprint(args ...any) {
	streams.Stderr.Print(args...)
}
