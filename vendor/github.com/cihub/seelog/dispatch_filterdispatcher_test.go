// Copyright (c) 2012 - Cloud Instruments Co., Ltd.
//
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are met:
//
// 1. Redistributions of source code must retain the above copyright notice, this
//    list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright notice,
//    this list of conditions and the following disclaimer in the documentation
//    and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
// WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
// DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR
// ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
// (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
// LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
// ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
// SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package seelog

import (
	"testing"
)

func TestfilterDispatcher_Pass(t *testing.T) {
	writer, _ := newBytesVerifier(t)
	filter, err := NewFilterDispatcher(onlyMessageFormatForTest, []interface{}{writer}, TraceLvl)
	if err != nil {
		t.Error(err)
		return
	}

	context, err := currentContext(nil)
	if err != nil {
		t.Error(err)
		return
	}

	bytes := []byte("Hello")
	writer.ExpectBytes(bytes)
	filter.Dispatch(string(bytes), TraceLvl, context, func(err error) {})
	writer.MustNotExpect()
}

func TestfilterDispatcher_Deny(t *testing.T) {
	writer, _ := newBytesVerifier(t)
	filter, err := NewFilterDispatcher(DefaultFormatter, []interface{}{writer})
	if err != nil {
		t.Error(err)
		return
	}

	context, err := currentContext(nil)
	if err != nil {
		t.Error(err)
		return
	}

	bytes := []byte("Hello")
	filter.Dispatch(string(bytes), TraceLvl, context, func(err error) {})
}
