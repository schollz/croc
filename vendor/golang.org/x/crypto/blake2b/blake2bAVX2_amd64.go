// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build go1.7,amd64,!gccgo,!appengine

package blake2b

import _ "unsafe"

//go:linkname x86_HasAVX internal/cpu.X86.HasAVX
var x86_HasAVX bool

//go:linkname x86_HasAVX2 internal/cpu.X86.HasAVX2
var x86_HasAVX2 bool

//go:linkname x86_HasAVX internal/cpu.X86.HasSSE4
var x86_HasSSE4 bool

func init() {
	useAVX2 = x86_HasAVX2
	useAVX = x86_HasAVX
	useSSE4 = x86_HasSSE4
}

//go:noescape
func hashBlocksAVX2(h *[8]uint64, c *[2]uint64, flag uint64, blocks []byte)

//go:noescape
func hashBlocksAVX(h *[8]uint64, c *[2]uint64, flag uint64, blocks []byte)

//go:noescape
func hashBlocksSSE4(h *[8]uint64, c *[2]uint64, flag uint64, blocks []byte)

func hashBlocks(h *[8]uint64, c *[2]uint64, flag uint64, blocks []byte) {
	if useAVX2 {
		hashBlocksAVX2(h, c, flag, blocks)
	} else if useAVX {
		hashBlocksAVX(h, c, flag, blocks)
	} else if useSSE4 {
		hashBlocksSSE4(h, c, flag, blocks)
	} else {
		hashBlocksGeneric(h, c, flag, blocks)
	}
}
