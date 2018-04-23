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
	"errors"
	"strconv"
	"testing"
)

// bytesVerifier is a byte receiver which is used for correct input testing.
// It allows to compare expected result and actual result in context of received bytes.
type bytesVerifier struct {
	expectedBytes   []byte // bytes that are expected to be written in next Write call
	waitingForInput bool   // true if verifier is waiting for a Write call
	writtenData     []byte // real bytes that actually were received during the last Write call
	testEnv         *testing.T
}

func newBytesVerifier(t *testing.T) (*bytesVerifier, error) {
	if t == nil {
		return nil, errors.New("testing environment param is nil")
	}

	verifier := new(bytesVerifier)
	verifier.testEnv = t

	return verifier, nil
}

// Write is used to check whether verifier was waiting for input and whether bytes are the same as expectedBytes.
// After Write call, waitingForInput is set to false.
func (verifier *bytesVerifier) Write(bytes []byte) (n int, err error) {
	if !verifier.waitingForInput {
		verifier.testEnv.Errorf("unexpected input: %v", string(bytes))
		return
	}

	verifier.waitingForInput = false
	verifier.writtenData = bytes

	if verifier.expectedBytes != nil {
		if bytes == nil {
			verifier.testEnv.Errorf("incoming 'bytes' is nil")
		} else {
			if len(bytes) != len(verifier.expectedBytes) {
				verifier.testEnv.Errorf("'Bytes' has unexpected len. Expected: %d. Got: %d. . Expected string: %q. Got: %q",
					len(verifier.expectedBytes), len(bytes), string(verifier.expectedBytes), string(bytes))
			} else {
				for i := 0; i < len(bytes); i++ {
					if verifier.expectedBytes[i] != bytes[i] {
						verifier.testEnv.Errorf("incorrect data on position %d. Expected: %d. Got: %d. Expected string: %q. Got: %q",
							i, verifier.expectedBytes[i], bytes[i], string(verifier.expectedBytes), string(bytes))
						break
					}
				}
			}
		}
	}

	return len(bytes), nil
}

func (verifier *bytesVerifier) ExpectBytes(bytes []byte) {
	verifier.waitingForInput = true
	verifier.expectedBytes = bytes
}

func (verifier *bytesVerifier) MustNotExpect() {
	if verifier.waitingForInput {
		errorText := "Unexpected input: "

		if verifier.expectedBytes != nil {
			errorText += "len = " + strconv.Itoa(len(verifier.expectedBytes))
			errorText += ". text = " + string(verifier.expectedBytes)
		}

		verifier.testEnv.Errorf(errorText)
	}
}

func (verifier *bytesVerifier) Close() error {
	return nil
}

// nullWriter implements io.Writer inteface and does nothing, always returning a successful write result
type nullWriter struct {
}

func (writer *nullWriter) Write(bytes []byte) (n int, err error) {
	return len(bytes), nil
}

func (writer *nullWriter) Close() error {
	return nil
}
