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

func TestInvalidminMaxConstraints(t *testing.T) {
	constr, err := NewMinMaxConstraints(CriticalLvl, WarnLvl)

	if err == nil || constr != nil {
		t.Errorf("expected an error and a nil value for minmax constraints: min = %d, max = %d. Got: %v, %v",
			CriticalLvl, WarnLvl, err, constr)
		return
	}
}

func TestInvalidLogLevels(t *testing.T) {
	var invalidMin uint8 = 123
	var invalidMax uint8 = 124
	minMaxConstr, errMinMax := NewMinMaxConstraints(LogLevel(invalidMin), LogLevel(invalidMax))

	if errMinMax == nil || minMaxConstr != nil {
		t.Errorf("expected an error and a nil value for minmax constraints: min = %d, max = %d. Got: %v, %v",
			invalidMin, invalidMax, errMinMax, minMaxConstr)
		return
	}

	invalidList := []LogLevel{145}

	listConstr, errList := NewListConstraints(invalidList)

	if errList == nil || listConstr != nil {
		t.Errorf("expected an error and a nil value for constraints list: %v. Got: %v, %v",
			invalidList, errList, listConstr)
		return
	}
}

func TestlistConstraintsWithDuplicates(t *testing.T) {
	duplicateList := []LogLevel{TraceLvl, DebugLvl, InfoLvl,
		WarnLvl, ErrorLvl, CriticalLvl, CriticalLvl, CriticalLvl}

	listConstr, errList := NewListConstraints(duplicateList)

	if errList != nil || listConstr == nil {
		t.Errorf("expected a valid constraints list struct for: %v, got error: %v, value: %v",
			duplicateList, errList, listConstr)
		return
	}

	listLevels := listConstr.AllowedLevels()

	if listLevels == nil {
		t.Fatalf("listConstr.AllowedLevels() == nil")
		return
	}

	if len(listLevels) != 6 {
		t.Errorf("expected: listConstr.AllowedLevels() length == 6. Got: %d", len(listLevels))
		return
	}
}

func TestlistConstraintsWithOffInList(t *testing.T) {
	offList := []LogLevel{TraceLvl, DebugLvl, Off}

	listConstr, errList := NewListConstraints(offList)

	if errList == nil || listConstr != nil {
		t.Errorf("expected an error and a nil value for constraints list with 'Off':  %v. Got: %v, %v",
			offList, errList, listConstr)
		return
	}
}

type logLevelTestCase struct {
	level   LogLevel
	allowed bool
}

var minMaxTests = []logLevelTestCase{
	{TraceLvl, false},
	{DebugLvl, false},
	{InfoLvl, true},
	{WarnLvl, true},
	{ErrorLvl, false},
	{CriticalLvl, false},
	{123, false},
	{6, false},
}

func TestValidminMaxConstraints(t *testing.T) {

	constr, err := NewMinMaxConstraints(InfoLvl, WarnLvl)

	if err != nil || constr == nil {
		t.Errorf("expected a valid constraints struct for minmax constraints: min = %d, max = %d. Got: %v, %v",
			InfoLvl, WarnLvl, err, constr)
		return
	}

	for _, minMaxTest := range minMaxTests {
		allowed := constr.IsAllowed(minMaxTest.level)
		if allowed != minMaxTest.allowed {
			t.Errorf("expected IsAllowed() = %t for level = %d. Got: %t",
				minMaxTest.allowed, minMaxTest.level, allowed)
			return
		}
	}
}

var listTests = []logLevelTestCase{
	{TraceLvl, true},
	{DebugLvl, false},
	{InfoLvl, true},
	{WarnLvl, true},
	{ErrorLvl, false},
	{CriticalLvl, true},
	{123, false},
	{6, false},
}

func TestValidlistConstraints(t *testing.T) {
	validList := []LogLevel{TraceLvl, InfoLvl, WarnLvl, CriticalLvl}
	constr, err := NewListConstraints(validList)

	if err != nil || constr == nil {
		t.Errorf("expected a valid constraints list struct for: %v. Got error: %v, value: %v",
			validList, err, constr)
		return
	}

	for _, minMaxTest := range listTests {
		allowed := constr.IsAllowed(minMaxTest.level)
		if allowed != minMaxTest.allowed {
			t.Errorf("expected IsAllowed() = %t for level = %d. Got: %t",
				minMaxTest.allowed, minMaxTest.level, allowed)
			return
		}
	}
}

var offTests = []logLevelTestCase{
	{TraceLvl, false},
	{DebugLvl, false},
	{InfoLvl, false},
	{WarnLvl, false},
	{ErrorLvl, false},
	{CriticalLvl, false},
	{123, false},
	{6, false},
}

func TestValidListoffConstraints(t *testing.T) {
	validList := []LogLevel{Off}
	constr, err := NewListConstraints(validList)

	if err != nil || constr == nil {
		t.Errorf("expected a valid constraints list struct for: %v. Got error: %v, value: %v",
			validList, err, constr)
		return
	}

	for _, minMaxTest := range offTests {
		allowed := constr.IsAllowed(minMaxTest.level)
		if allowed != minMaxTest.allowed {
			t.Errorf("expected IsAllowed() = %t for level = %d. Got: %t",
				minMaxTest.allowed, minMaxTest.level, allowed)
			return
		}
	}
}
