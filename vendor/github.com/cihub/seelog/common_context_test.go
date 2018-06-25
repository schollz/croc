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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testShortPath = "common_context_test.go"
)

var (
	commonPrefix string
	testFullPath string
)

func init() {
	// Here we remove the hardcoding of the package name which
	// may break forks and some CI environments such as jenkins.
	_, _, funcName, _, _ := extractCallerInfo(1)
	preIndex := strings.Index(funcName, "init·")
	if preIndex == -1 {
		preIndex = strings.Index(funcName, "init")
	}
	commonPrefix = funcName[:preIndex]
	wd, err := os.Getwd()
	if err == nil {
		// Transform the file path into a slashed form:
		// This is the proper platform-neutral way.
		testFullPath = filepath.ToSlash(filepath.Join(wd, testShortPath))
	}
}

func TestContext(t *testing.T) {
	context, err := currentContext(nil)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if context == nil {
		t.Fatalf("unexpected error: context is nil")
	}
	if fn, funcName := context.Func(), commonPrefix+"TestContext"; fn != funcName {
		// Account for a case when the func full path is longer than commonPrefix but includes it.
		if !strings.HasSuffix(fn, funcName) {
			t.Errorf("expected context.Func == %s ; got %s", funcName, context.Func())
		}
	}
	if context.ShortPath() != testShortPath {
		t.Errorf("expected context.ShortPath == %s ; got %s", testShortPath, context.ShortPath())
	}
	if len(testFullPath) == 0 {
		t.Fatal("working directory seems invalid")
	}
	if context.FullPath() != testFullPath {
		t.Errorf("expected context.FullPath == %s ; got %s", testFullPath, context.FullPath())
	}
}

func innerContext() (context LogContextInterface, err error) {
	return currentContext(nil)
}

func TestInnerContext(t *testing.T) {
	context, err := innerContext()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if context == nil {
		t.Fatalf("unexpected error: context is nil")
	}
	if fn, funcName := context.Func(), commonPrefix+"innerContext"; fn != funcName {
		// Account for a case when the func full path is longer than commonPrefix but includes it.
		if !strings.HasSuffix(fn, funcName) {
			t.Errorf("expected context.Func == %s ; got %s", funcName, context.Func())
		}
	}
	if context.ShortPath() != testShortPath {
		t.Errorf("expected context.ShortPath == %s ; got %s", testShortPath, context.ShortPath())
	}
	if len(testFullPath) == 0 {
		t.Fatal("working directory seems invalid")
	}
	if context.FullPath() != testFullPath {
		t.Errorf("expected context.FullPath == %s ; got %s", testFullPath, context.FullPath())
	}
}

type testContext struct {
	field string
}

func TestCustomContext(t *testing.T) {
	expected := "testStr"
	context, err := currentContext(&testContext{expected})
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if st, _ := context.CustomContext().(*testContext); st.field != expected {
		t.Errorf("expected context.CustomContext == %s ; got %s", expected, st.field)
	}
}
