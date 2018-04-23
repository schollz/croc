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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	messageLen = 10
)

var bytesFileTest = []byte(strings.Repeat("A", messageLen))

func TestSimpleFileWriter(t *testing.T) {
	t.Logf("Starting file writer tests")
	NewFileWriterTester(simplefileWriterTests, simplefileWriterGetter, t).test()
}

//===============================================================

func simplefileWriterGetter(testCase *fileWriterTestCase) (io.WriteCloser, error) {
	return NewFileWriter(testCase.fileName)
}

//===============================================================
type fileWriterTestCase struct {
	files       []string
	fileName    string
	rollingType rollingType
	fileSize    int64
	maxRolls    int
	datePattern string
	writeCount  int
	resFiles    []string
	nameMode    rollingNameMode
}

func createSimplefileWriterTestCase(fileName string, writeCount int) *fileWriterTestCase {
	return &fileWriterTestCase{[]string{}, fileName, rollingTypeSize, 0, 0, "", writeCount, []string{fileName}, 0}
}

var simplefileWriterTests = []*fileWriterTestCase{
	createSimplefileWriterTestCase("log.testlog", 1),
	createSimplefileWriterTestCase("log.testlog", 50),
	createSimplefileWriterTestCase(filepath.Join("dir", "log.testlog"), 50),
}

//===============================================================

type fileWriterTester struct {
	testCases    []*fileWriterTestCase
	writerGetter func(*fileWriterTestCase) (io.WriteCloser, error)
	t            *testing.T
}

func NewFileWriterTester(
	testCases []*fileWriterTestCase,
	writerGetter func(*fileWriterTestCase) (io.WriteCloser, error),
	t *testing.T) *fileWriterTester {

	return &fileWriterTester{testCases, writerGetter, t}
}

func isWriterTestFile(fn string) bool {
	return strings.Contains(fn, ".testlog")
}

func cleanupWriterTest(t *testing.T) {
	toDel, err := getDirFilePaths(".", isWriterTestFile, true)
	if nil != err {
		t.Fatal("Cannot list files in test directory!")
	}

	for _, p := range toDel {
		if err = tryRemoveFile(p); nil != err {
			t.Errorf("cannot remove file %s in test directory: %s", p, err.Error())
		}
	}

	if err = os.RemoveAll("dir"); nil != err {
		t.Errorf("cannot remove temp test directory: %s", err.Error())
	}
}

func getWriterTestResultFiles() ([]string, error) {
	var p []string

	visit := func(path string, f os.FileInfo, err error) error {
		if !f.IsDir() && isWriterTestFile(path) {
			abs, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("filepath.Abs failed for %s", path)
			}

			p = append(p, abs)
		}

		return nil
	}

	err := filepath.Walk(".", visit)
	if nil != err {
		return nil, err
	}

	return p, nil
}

func (tester *fileWriterTester) testCase(testCase *fileWriterTestCase, testNum int) {
	defer cleanupWriterTest(tester.t)

	tester.t.Logf("Start test  [%v]\n", testNum)

	for _, filePath := range testCase.files {
		dir, _ := filepath.Split(filePath)

		var err error

		if 0 != len(dir) {
			err = os.MkdirAll(dir, defaultDirectoryPermissions)
			if err != nil {
				tester.t.Error(err)
				return
			}
		}

		fi, err := os.Create(filePath)
		if err != nil {
			tester.t.Error(err)
			return
		}

		err = fi.Close()
		if err != nil {
			tester.t.Error(err)
			return
		}
	}

	fwc, err := tester.writerGetter(testCase)
	if err != nil {
		tester.t.Error(err)
		return
	}
	defer fwc.Close()

	tester.performWrite(fwc, testCase.writeCount)

	files, err := getWriterTestResultFiles()
	if err != nil {
		tester.t.Error(err)
		return
	}

	tester.checkRequiredFilesExist(testCase, files)
	tester.checkJustRequiredFilesExist(testCase, files)

}

func (tester *fileWriterTester) test() {
	for i, tc := range tester.testCases {
		cleanupWriterTest(tester.t)
		tester.testCase(tc, i)
	}
}

func (tester *fileWriterTester) performWrite(fileWriter io.Writer, count int) {
	for i := 0; i < count; i++ {
		_, err := fileWriter.Write(bytesFileTest)

		if err != nil {
			tester.t.Error(err)
			return
		}
	}
}

func (tester *fileWriterTester) checkRequiredFilesExist(testCase *fileWriterTestCase, files []string) {
	var found bool
	for _, expected := range testCase.resFiles {
		found = false
		exAbs, err := filepath.Abs(expected)
		if err != nil {
			tester.t.Errorf("filepath.Abs failed for %s", expected)
			continue
		}

		for _, f := range files {
			if af, e := filepath.Abs(f); e == nil {
				tester.t.Log(af)
				if exAbs == af {
					found = true
					break
				}
			} else {
				tester.t.Errorf("filepath.Abs failed for %s", f)
			}
		}

		if !found {
			tester.t.Errorf("expected file: %s doesn't exist. Got %v\n", exAbs, files)
		}
	}
}

func (tester *fileWriterTester) checkJustRequiredFilesExist(testCase *fileWriterTestCase, files []string) {
	for _, f := range files {
		found := false
		for _, expected := range testCase.resFiles {

			exAbs, err := filepath.Abs(expected)
			if err != nil {
				tester.t.Errorf("filepath.Abs failed for %s", expected)
			} else {
				if exAbs == f {
					found = true
					break
				}
			}
		}

		if !found {
			tester.t.Errorf("unexpected file: %v", f)
		}
	}
}
