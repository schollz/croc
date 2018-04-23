// Copyright (c) 2013 - Cloud Instruments Co., Ltd.
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

type testCustomDispatcherMessageReceiver struct {
	customTestReceiver
}

func TestCustomDispatcher_Message(t *testing.T) {
	recName := "TestCustomDispatcher_Message"
	RegisterReceiver(recName, &testCustomDispatcherMessageReceiver{})

	customDispatcher, err := NewCustomReceiverDispatcher(onlyMessageFormatForTest, recName, CustomReceiverInitArgs{
		XmlCustomAttrs: map[string]string{
			"test": "testdata",
		},
	})
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
	customDispatcher.Dispatch(string(bytes), TraceLvl, context, func(err error) {})

	cout := customDispatcher.innerReceiver.(*testCustomDispatcherMessageReceiver).customTestReceiver.co
	if cout.initCalled != true {
		t.Error("Init not called")
		return
	}
	if cout.dataPassed != "testdata" {
		t.Errorf("wrong data passed: '%s'", cout.dataPassed)
		return
	}
	if cout.messageOutput != string(bytes) {
		t.Errorf("wrong message output: '%s'", cout.messageOutput)
		return
	}
	if cout.levelOutput != TraceLvl {
		t.Errorf("wrong log level: '%s'", cout.levelOutput)
		return
	}
	if cout.flushed {
		t.Error("Flush was not expected")
		return
	}
	if cout.closed {
		t.Error("Closing was not expected")
		return
	}
}

type testCustomDispatcherFlushReceiver struct {
	customTestReceiver
}

func TestCustomDispatcher_Flush(t *testing.T) {
	recName := "TestCustomDispatcher_Flush"
	RegisterReceiver(recName, &testCustomDispatcherFlushReceiver{})

	customDispatcher, err := NewCustomReceiverDispatcher(onlyMessageFormatForTest, recName, CustomReceiverInitArgs{
		XmlCustomAttrs: map[string]string{
			"test": "testdata",
		},
	})
	if err != nil {
		t.Error(err)
		return
	}

	customDispatcher.Flush()

	cout := customDispatcher.innerReceiver.(*testCustomDispatcherFlushReceiver).customTestReceiver.co
	if cout.initCalled != true {
		t.Error("Init not called")
		return
	}
	if cout.dataPassed != "testdata" {
		t.Errorf("wrong data passed: '%s'", cout.dataPassed)
		return
	}
	if cout.messageOutput != "" {
		t.Errorf("wrong message output: '%s'", cout.messageOutput)
		return
	}
	if cout.levelOutput != TraceLvl {
		t.Errorf("wrong log level: '%s'", cout.levelOutput)
		return
	}
	if !cout.flushed {
		t.Error("Flush was expected")
		return
	}
	if cout.closed {
		t.Error("Closing was not expected")
		return
	}
}

type testCustomDispatcherCloseReceiver struct {
	customTestReceiver
}

func TestCustomDispatcher_Close(t *testing.T) {
	recName := "TestCustomDispatcher_Close"
	RegisterReceiver(recName, &testCustomDispatcherCloseReceiver{})

	customDispatcher, err := NewCustomReceiverDispatcher(onlyMessageFormatForTest, recName, CustomReceiverInitArgs{
		XmlCustomAttrs: map[string]string{
			"test": "testdata",
		},
	})
	if err != nil {
		t.Error(err)
		return
	}

	customDispatcher.Close()

	cout := customDispatcher.innerReceiver.(*testCustomDispatcherCloseReceiver).customTestReceiver.co
	if cout.initCalled != true {
		t.Error("Init not called")
		return
	}
	if cout.dataPassed != "testdata" {
		t.Errorf("wrong data passed: '%s'", cout.dataPassed)
		return
	}
	if cout.messageOutput != "" {
		t.Errorf("wrong message output: '%s'", cout.messageOutput)
		return
	}
	if cout.levelOutput != TraceLvl {
		t.Errorf("wrong log level: '%s'", cout.levelOutput)
		return
	}
	if !cout.flushed {
		t.Error("Flush was expected")
		return
	}
	if !cout.closed {
		t.Error("Closing was expected")
		return
	}
}
