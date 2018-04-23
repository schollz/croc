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
	"strings"
	"testing"
)

func TestConfig(t *testing.T) {
	testConfig :=
		`
<seelog levels="trace, debug">
	<exceptions>
		<exception funcpattern="*getFirst*" filepattern="*" minlevel="off" />
		<exception funcpattern="*getSecond*" filepattern="*" levels="info, error" />
	</exceptions>
</seelog>
`

	conf, err := configFromReader(strings.NewReader(testConfig))
	if err != nil {
		t.Errorf("parse error: %s\n", err.Error())
		return
	}

	context, err := currentContext(nil)
	if err != nil {
		t.Errorf("cannot get current context:" + err.Error())
		return
	}
	firstContext, err := getFirstContext()
	if err != nil {
		t.Errorf("cannot get current context:" + err.Error())
		return
	}
	secondContext, err := getSecondContext()
	if err != nil {
		t.Errorf("cannot get current context:" + err.Error())
		return
	}

	if !conf.IsAllowed(TraceLvl, context) {
		t.Errorf("error: deny trace in current context")
	}
	if conf.IsAllowed(TraceLvl, firstContext) {
		t.Errorf("error: allow trace in first context")
	}
	if conf.IsAllowed(ErrorLvl, context) {
		t.Errorf("error: allow error in current context")
	}
	if !conf.IsAllowed(ErrorLvl, secondContext) {
		t.Errorf("error: deny error in second context")
	}

	// cache test
	if !conf.IsAllowed(TraceLvl, context) {
		t.Errorf("error: deny trace in current context")
	}
	if conf.IsAllowed(TraceLvl, firstContext) {
		t.Errorf("error: allow trace in first context")
	}
	if conf.IsAllowed(ErrorLvl, context) {
		t.Errorf("error: allow error in current context")
	}
	if !conf.IsAllowed(ErrorLvl, secondContext) {
		t.Errorf("error: deny error in second context")
	}
}

func getFirstContext() (LogContextInterface, error) {
	return currentContext(nil)
}

func getSecondContext() (LogContextInterface, error) {
	return currentContext(nil)
}
