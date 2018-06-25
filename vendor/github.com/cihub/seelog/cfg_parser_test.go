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
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type customTestReceiverOutput struct {
	initCalled    bool
	dataPassed    string
	messageOutput string
	levelOutput   LogLevel
	closed        bool
	flushed       bool
}
type customTestReceiver struct{ co *customTestReceiverOutput }

func (cr *customTestReceiver) ReceiveMessage(message string, level LogLevel, context LogContextInterface) error {
	cr.co.messageOutput = message
	cr.co.levelOutput = level
	return nil
}

func (cr *customTestReceiver) String() string {
	return fmt.Sprintf("custom data='%s'", cr.co.dataPassed)
}

func (cr *customTestReceiver) AfterParse(initArgs CustomReceiverInitArgs) error {
	cr.co = new(customTestReceiverOutput)
	cr.co.initCalled = true
	cr.co.dataPassed = initArgs.XmlCustomAttrs["test"]
	return nil
}

func (cr *customTestReceiver) Flush() {
	cr.co.flushed = true
}

func (cr *customTestReceiver) Close() error {
	cr.co.closed = true
	return nil
}

var re = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func getTestFileName(testName, postfix string) string {
	if len(postfix) != 0 {
		return strings.ToLower(re.ReplaceAllString(testName, "_")) + "_" + postfix + "_test.log"
	}
	return strings.ToLower(re.ReplaceAllString(testName, "_")) + "_test.log"
}

var parserTests []parserTest

type parserTest struct {
	testName      string
	config        string
	expected      *configForParsing //interface{}
	errorExpected bool
	parserConfig  *CfgParseParams
}

func getParserTests() []parserTest {
	if parserTests == nil {
		parserTests = make([]parserTest, 0)

		testName := "Simple file output"
		testLogFileName := getTestFileName(testName, "")
		testConfig := `
		<seelog>
			<outputs>
				<file path="` + testLogFileName + `"/>
			</outputs>
		</seelog>
		`
		testExpected := new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testfileWriter, _ := NewFileWriter(testLogFileName)
		testHeadSplitter, _ := NewSplitDispatcher(DefaultFormatter, []interface{}{testfileWriter})
		testExpected.LogType = asyncLooploggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Filter dispatcher"
		testLogFileName = getTestFileName(testName, "")
		testConfig = `
		<seelog type="sync">
			<outputs>
				<filter levels="debug, info, critical">
					<file path="` + testLogFileName + `"/>
				</filter>
			</outputs>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testfileWriter, _ = NewFileWriter(testLogFileName)
		testFilter, _ := NewFilterDispatcher(DefaultFormatter, []interface{}{testfileWriter}, DebugLvl, InfoLvl, CriticalLvl)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testFilter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Console writer"
		testConfig = `
		<seelog type="sync">
			<outputs>
				<console />
			</outputs>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testconsoleWriter, _ := NewConsoleWriter()
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testconsoleWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "SMTP writer"
		testConfig = `
<seelog>
	<outputs>
		<smtp senderaddress="sa" sendername="sn"  hostname="hn" hostport="123" username="un" password="up">
			<recipient address="ra1"/>
			<recipient address="ra2"/>
			<recipient address="ra3"/>
			<cacertdirpath path="cacdp1"/>
			<cacertdirpath path="cacdp2"/>
		</smtp>
	</outputs>
</seelog>
		`

		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testSMTPWriter := NewSMTPWriter(
			"sa",
			"sn",
			[]string{"ra1", "ra2", "ra3"},
			"hn",
			"123",
			"un",
			"up",
			[]string{"cacdp1", "cacdp2"},
			DefaultSubjectPhrase,
			nil,
		)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testSMTPWriter})
		testExpected.LogType = asyncLooploggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "SMTP writer custom header and subject configuration"
		testConfig = `
<seelog>
	<outputs>
		<smtp senderaddress="sa" sendername="sn"  hostname="hn" hostport="123" username="un" password="up" subject="ohlala">
			<recipient address="ra1"/>
			<cacertdirpath path="cacdp1"/>
			<header name="Priority" value="Urgent" />
			<header name="Importance" value="high" />
			<header name="Sensitivity" value="Company-Confidential" />
			<header name="Auto-Submitted" value="auto-generated" />
		</smtp>
	</outputs>
</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testSMTPWriter = NewSMTPWriter(
			"sa",
			"sn",
			[]string{"ra1"},
			"hn",
			"123",
			"un",
			"up",
			[]string{"cacdp1"},
			"ohlala",
			[]string{"Priority: Urgent", "Importance: high", "Sensitivity: Company-Confidential", "Auto-Submitted: auto-generated"},
		)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testSMTPWriter})
		testExpected.LogType = asyncLooploggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Default output"
		testConfig = `
		<seelog type="sync"/>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testconsoleWriter, _ = NewConsoleWriter()
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testconsoleWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Asyncloop behavior"
		testConfig = `
		<seelog type="asyncloop"/>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testconsoleWriter, _ = NewConsoleWriter()
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testconsoleWriter})
		testExpected.LogType = asyncLooploggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Asynctimer behavior"
		testConfig = `
		<seelog type="asynctimer" asyncinterval="101"/>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testconsoleWriter, _ = NewConsoleWriter()
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testconsoleWriter})
		testExpected.LogType = asyncTimerloggerTypeFromString
		testExpected.LoggerData = asyncTimerLoggerData{101}
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Rolling file writer size"
		testLogFileName = getTestFileName(testName, "")
		testConfig = `
		<seelog type="sync">
			<outputs>
				<rollingfile type="size" filename="` + testLogFileName + `" maxsize="100" maxrolls="5" />
			</outputs>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testrollingFileWriter, _ := NewRollingFileWriterSize(testLogFileName, rollingArchiveNone, "", 100, 5, rollingNameModePostfix)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testrollingFileWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Rolling file writer archive zip"
		testLogFileName = getTestFileName(testName, "")
		testConfig = `
		<seelog type="sync">
			<outputs>
				<rollingfile type="size" filename="` + testLogFileName + `" maxsize="100" maxrolls="5" archivetype="zip"/>
			</outputs>
		</seelog>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testrollingFileWriter, _ = NewRollingFileWriterSize(testLogFileName, rollingArchiveZip, "log.zip", 100, 5, rollingNameModePostfix)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testrollingFileWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Rolling file writer archive zip with specified path"
		testLogFileName = getTestFileName(testName, "")
		testConfig = `
		<seelog type="sync">
			<outputs>
				<rollingfile namemode="prefix" type="size" filename="` + testLogFileName + `" maxsize="100" maxrolls="5" archivetype="zip" archivepath="test.zip"/>
			</outputs>
		</seelog>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testrollingFileWriter, _ = NewRollingFileWriterSize(testLogFileName, rollingArchiveZip, "test.zip", 100, 5, rollingNameModePrefix)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testrollingFileWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Rolling file writer archive none"
		testLogFileName = getTestFileName(testName, "")
		testConfig = `
		<seelog type="sync">
			<outputs>
				<rollingfile namemode="postfix" type="size" filename="` + testLogFileName + `" maxsize="100" maxrolls="5" archivetype="none"/>
			</outputs>
		</seelog>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testrollingFileWriter, _ = NewRollingFileWriterSize(testLogFileName, rollingArchiveNone, "", 100, 5, rollingNameModePostfix)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testrollingFileWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Rolling file writer date"
		testLogFileName = getTestFileName(testName, "")
		testConfig = `
		<seelog type="sync">
			<outputs>
				<rollingfile type="date" filename="` + testLogFileName + `" datepattern="2006-01-02T15:04:05Z07:00" />
			</outputs>
		</seelog>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testrollingFileWriterTime, _ := NewRollingFileWriterTime(testLogFileName, rollingArchiveNone, "", 0, "2006-01-02T15:04:05Z07:00", rollingIntervalAny, rollingNameModePostfix)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testrollingFileWriterTime})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Buffered writer"
		testLogFileName = getTestFileName(testName, "")
		testConfig = `
		<seelog type="sync">
			<outputs>
				<buffered size="100500" flushperiod="100">
					<rollingfile type="date" filename="` + testLogFileName + `" datepattern="2006-01-02T15:04:05Z07:00" />
				</buffered>
			</outputs>
		</seelog>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testrollingFileWriterTime, _ = NewRollingFileWriterTime(testLogFileName, rollingArchiveNone, "", 0, "2006-01-02T15:04:05Z07:00", rollingIntervalDaily, rollingNameModePostfix)
		testbufferedWriter, _ := NewBufferedWriter(testrollingFileWriterTime, 100500, 100)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testbufferedWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Inner splitter output"
		testLogFileName1 := getTestFileName(testName, "1")
		testLogFileName2 := getTestFileName(testName, "2")
		testLogFileName3 := getTestFileName(testName, "3")
		testConfig = `
		<seelog type="sync">
			<outputs>
				<file path="` + testLogFileName1 + `"/>
				<splitter>
					<file path="` + testLogFileName2 + `"/>
					<file path="` + testLogFileName3 + `"/>
				</splitter>
			</outputs>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testfileWriter1, _ := NewFileWriter(testLogFileName2)
		testfileWriter2, _ := NewFileWriter(testLogFileName3)
		testInnerSplitter, _ := NewSplitDispatcher(DefaultFormatter, []interface{}{testfileWriter1, testfileWriter2})
		testfileWriter, _ = NewFileWriter(testLogFileName1)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testfileWriter, testInnerSplitter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		RegisterReceiver("custom-name-1", &customTestReceiver{})

		testName = "Custom receiver 1"
		testConfig = `
		<seelog type="sync">
			<outputs>
				<custom name="custom-name-1" data-test="set"/>
			</outputs>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testCustomReceiver, _ := NewCustomReceiverDispatcher(DefaultFormatter, "custom-name-1", CustomReceiverInitArgs{
			XmlCustomAttrs: map[string]string{
				"test": "set",
			},
		})
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testCustomReceiver})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Custom receiver 2"
		testConfig = `
		<seelog type="sync">
			<outputs>
				<custom name="custom-name-2" data-test="set2"/>
			</outputs>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		crec := &customTestReceiver{}
		cargs := CustomReceiverInitArgs{
			XmlCustomAttrs: map[string]string{
				"test": "set2",
			},
		}
		crec.AfterParse(cargs)
		testCustomReceiver2, _ := NewCustomReceiverDispatcherByValue(DefaultFormatter, crec, "custom-name-2", cargs)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testCustomReceiver2})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		fnc := func(initArgs CustomReceiverInitArgs) (CustomReceiver, error) {
			return &customTestReceiver{}, nil
		}
		cfg := CfgParseParams{
			CustomReceiverProducers: map[string]CustomReceiverProducer{
				"custom-name-2": CustomReceiverProducer(fnc),
			},
		}
		testExpected.Params = &cfg
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, &cfg})

		RegisterReceiver("-", &customTestReceiver{})
		testName = "Custom receiver 3"
		testConfig = `
		<seelog type="sync">
			<outputs>
				<custom name="-" data-test="set3"/>
			</outputs>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		creccustom := &customTestReceiver{}
		cargs3 := CustomReceiverInitArgs{
			XmlCustomAttrs: map[string]string{
				"test": "set3",
			},
		}
		creccustom.AfterParse(cargs3)
		testCustomReceiver, _ = NewCustomReceiverDispatcherByValue(DefaultFormatter, creccustom, "-", cargs3)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testCustomReceiver})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Custom receivers with formats"
		testConfig = `
		<seelog type="sync">
			<outputs>
				<custom name="custom-name-1" data-test="set1"/>
				<custom name="custom-name-1" data-test="set2"/>
				<custom name="custom-name-1" data-test="set3"/>
			</outputs>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testCustomReceivers := make([]*customReceiverDispatcher, 3)
		for i := 0; i < 3; i++ {
			testCustomReceivers[i], _ = NewCustomReceiverDispatcher(DefaultFormatter, "custom-name-1", CustomReceiverInitArgs{
				XmlCustomAttrs: map[string]string{
					"test": fmt.Sprintf("set%d", i+1),
				},
			})
		}

		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testCustomReceivers[0], testCustomReceivers[1], testCustomReceivers[2]})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Format"
		testLogFileName = getTestFileName(testName, "")
		testConfig = `
		<seelog type="sync">
			<outputs formatid="dateFormat">
				<file path="` + testLogFileName + `"/>
			</outputs>
			<formats>
				<format id="dateFormat" format="%Level %Msg %File" />
			</formats>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testfileWriter, _ = NewFileWriter(testLogFileName)
		testFormat, _ := NewFormatter("%Level %Msg %File")
		testHeadSplitter, _ = NewSplitDispatcher(testFormat, []interface{}{testfileWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Format2"
		testLogFileName = getTestFileName(testName, "")
		testLogFileName1 = getTestFileName(testName, "1")
		testConfig = `
		<seelog type="sync">
			<outputs formatid="format1">
				<file path="` + testLogFileName + `"/>
				<file formatid="format2" path="` + testLogFileName1 + `"/>
			</outputs>
			<formats>
				<format id="format1" format="%Level %Msg %File" />
				<format id="format2" format="%l %Msg" />
			</formats>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testfileWriter, _ = NewFileWriter(testLogFileName)
		testfileWriter1, _ = NewFileWriter(testLogFileName1)
		testFormat1, _ := NewFormatter("%Level %Msg %File")
		testFormat2, _ := NewFormatter("%l %Msg")
		formattedWriter, _ := NewFormattedWriter(testfileWriter1, testFormat2)
		testHeadSplitter, _ = NewSplitDispatcher(testFormat1, []interface{}{testfileWriter, formattedWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Minlevel = warn"
		testConfig = `<seelog minlevel="warn"/>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(WarnLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testconsoleWriter, _ = NewConsoleWriter()
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testconsoleWriter})
		testExpected.LogType = asyncLooploggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Maxlevel = trace"
		testConfig = `<seelog maxlevel="trace"/>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, TraceLvl)
		testExpected.Exceptions = nil
		testconsoleWriter, _ = NewConsoleWriter()
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testconsoleWriter})
		testExpected.LogType = asyncLooploggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Level between info and error"
		testConfig = `<seelog minlevel="info" maxlevel="error"/>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(InfoLvl, ErrorLvl)
		testExpected.Exceptions = nil
		testconsoleWriter, _ = NewConsoleWriter()
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testconsoleWriter})
		testExpected.LogType = asyncLooploggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Off with minlevel"
		testConfig = `<seelog minlevel="off"/>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewOffConstraints()
		testExpected.Exceptions = nil
		testconsoleWriter, _ = NewConsoleWriter()
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testconsoleWriter})
		testExpected.LogType = asyncLooploggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Off with levels"
		testConfig = `<seelog levels="off"/>`
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Levels list"
		testConfig = `<seelog levels="debug, info, critical"/>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewListConstraints([]LogLevel{
			DebugLvl, InfoLvl, CriticalLvl})
		testExpected.Exceptions = nil
		testconsoleWriter, _ = NewConsoleWriter()
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testconsoleWriter})
		testExpected.LogType = asyncLooploggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Errors #1"
		testConfig = `<seelog minlevel="debug" minlevel="trace"/>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #2"
		testConfig = `<seelog minlevel="error" maxlevel="debug"/>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #3"
		testConfig = `<seelog maxlevel="debug" maxlevel="trace"/>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #4"
		testConfig = `<seelog maxlevel="off"/>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #5"
		testConfig = `<seelog minlevel="off" maxlevel="trace"/>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #6"
		testConfig = `<seelog minlevel="warn" maxlevel="error" levels="debug"/>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #7"
		testConfig = `<not_seelog/>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #8"
		testConfig = `<seelog levels="warn, debug, test"/>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #9"
		testConfig = `<seelog levels=""/>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #10"
		testConfig = `<seelog levels="off" something="abc"/>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #11"
		testConfig = `<seelog><output/></seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #12"
		testConfig = `<seelog><outputs/><outputs/></seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #13"
		testConfig = `<seelog><exceptions/></seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #14"
		testConfig = `<seelog><formats/></seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #15"
		testConfig = `<seelog><outputs><splitter/></outputs></seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #16"
		testConfig = `<seelog><outputs><filter/></outputs></seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #17"
		testLogFileName = getTestFileName(testName, "")
		testConfig = `<seelog><outputs><file path="` + testLogFileName + `"><something/></file></outputs></seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #18"
		testConfig = `<seelog><outputs><buffered size="100500" flushperiod="100"/></outputs></seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #19"
		testConfig = `<seelog><outputs></outputs></seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Exceptions: restricting"
		testConfig =
			`
		<seelog type="sync">
			<exceptions>
				<exception funcpattern="Test*" filepattern="someFile.go" minlevel="off"/>
			</exceptions>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		listConstraint, _ := NewOffConstraints()
		exception, _ := NewLogLevelException("Test*", "someFile.go", listConstraint)
		testExpected.Exceptions = []*LogLevelException{exception}
		testconsoleWriter, _ = NewConsoleWriter()
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testconsoleWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Exceptions: allowing #1"
		testConfig =
			`
		<seelog type="sync" levels="error">
			<exceptions>
				<exception filepattern="testfile.go" minlevel="trace"/>
			</exceptions>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewListConstraints([]LogLevel{ErrorLvl})
		minMaxConstraint, _ := NewMinMaxConstraints(TraceLvl, CriticalLvl)
		exception, _ = NewLogLevelException("*", "testfile.go", minMaxConstraint)
		testExpected.Exceptions = []*LogLevelException{exception}
		testconsoleWriter, _ = NewConsoleWriter()
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testconsoleWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Exceptions: allowing #2"
		testConfig = `
		<seelog type="sync" levels="off">
			<exceptions>
				<exception filepattern="testfile.go" minlevel="warn"/>
			</exceptions>
		</seelog>
		`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewOffConstraints()
		minMaxConstraint, _ = NewMinMaxConstraints(WarnLvl, CriticalLvl)
		exception, _ = NewLogLevelException("*", "testfile.go", minMaxConstraint)
		testExpected.Exceptions = []*LogLevelException{exception}
		testconsoleWriter, _ = NewConsoleWriter()
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testconsoleWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Predefined formats"
		formatID := predefinedPrefix + "xml-debug-short"
		testConfig = `
		<seelog type="sync">
			<outputs formatid="` + formatID + `">
				<console />
			</outputs>
		</seelog>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testconsoleWriter, _ = NewConsoleWriter()
		testFormat, _ = predefinedFormats[formatID]
		testHeadSplitter, _ = NewSplitDispatcher(testFormat, []interface{}{testconsoleWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Predefined formats redefine"
		testLogFileName = getTestFileName(testName, "")
		formatID = predefinedPrefix + "xml-debug-short"
		testConfig = `
		<seelog type="sync">
			<outputs formatid="` + formatID + `">
				<file path="` + testLogFileName + `"/>
			</outputs>
			<formats>
				<format id="` + formatID + `" format="%Level %Msg %File" />
			</formats>
		</seelog>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testfileWriter, _ = NewFileWriter(testLogFileName)
		testFormat, _ = NewFormatter("%Level %Msg %File")
		testHeadSplitter, _ = NewSplitDispatcher(testFormat, []interface{}{testfileWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Conn writer 1"
		testConfig = `
		<seelog type="sync">
			<outputs>
				<conn net="tcp" addr=":8888" />
			</outputs>
		</seelog>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testConnWriter := NewConnWriter("tcp", ":8888", false)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testConnWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Conn writer 2"
		testConfig = `
		<seelog type="sync">
			<outputs>
				<conn net="tcp" addr=":8888" reconnectonmsg="true" />
			</outputs>
		</seelog>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testConnWriter = NewConnWriter("tcp", ":8888", true)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{testConnWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

		testName = "Errors #11"
		testConfig = `
		<seelog type="sync"><exceptions>
				<exception filepattern="testfile.go" minlevel="trace"/>
				<exception filepattern="testfile.go" minlevel="warn"/>
		</exceptions></seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #12"
		testConfig = `
		<seelog type="sync"><exceptions>
				<exception filepattern="!@+$)!!%&@(^$" minlevel="trace"/>
		</exceptions></seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #13"
		testConfig = `
		<seelog type="sync"><exceptions>
				<exception filepattern="*" minlevel="unknown"/>
		</exceptions></seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #14"
		testConfig = `
		<seelog type="sync" levels=”off”>
			<exceptions>
				<exception filepattern="testfile.go" minlevel="off"/>
			</exceptions>
		</seelog>
		`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #15"
		testConfig = `
		<seelog type="sync" levels=”trace”>
			<exceptions>
				<exception filepattern="testfile.go" levels="trace"/>
			</exceptions>
		</seelog>
		`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #16"
		testConfig = `
		<seelog type="sync" minlevel=”trace”>
			<exceptions>
				<exception filepattern="testfile.go" minlevel="trace"/>
			</exceptions>
		</seelog>
		`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #17"
		testConfig = `
		<seelog type="sync" minlevel=”trace”>
			<exceptions>
				<exception filepattern="testfile.go" minlevel="warn"/>
			</exceptions>
			<exceptions>
				<exception filepattern="testfile.go" minlevel="warn"/>
			</exceptions>
		</seelog>
		`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #18"
		testConfig = `
		<seelog type="sync" minlevel=”trace”>
			<exceptions>
				<exception filepattern="testfile.go"/>
			</exceptions>
		</seelog>
		`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #19"
		testConfig = `
		<seelog type="sync" minlevel=”trace”>
			<exceptions>
				<exception minlevel="warn"/>
			</exceptions>
		</seelog>
		`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #20"
		testConfig = `
		<seelog type="sync" minlevel=”trace”>
			<exceptions>
				<exception/>
			</exceptions>
		</seelog>
		`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #21"
		testConfig = `
		<seelog>
			<outputs>
				<splitter>
				</splitter>
			</outputs>
		</seelog>
		`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #22"
		testConfig = `
		<seelog type="sync">
			<outputs>
				<filter levels="debug, info, critical">

				</filter>
			</outputs>
		</seelog>
		`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #23"
		testConfig = `
		<seelog type="sync">
			<outputs>
				<buffered size="100500" flushperiod="100">

				</buffered>
			</outputs>
		</seelog>
		`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #24"
		testLogFileName = getTestFileName(testName, "")
		testConfig = `
		<seelog type="sync">
			<outputs>
				<buffered size="100500" flushperiod="100">
					<rollingfile type="date" filename="` + testLogFileName + `" datepattern="2006-01-02T15:04:05Z07:00" formatid="testFormat"/>
				</buffered>
			</outputs>
			<formats>
				<format id="testFormat" format="%Level %Msg %File 123" />
			</formats>
		</seelog>
		`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #25"
		testLogFileName = getTestFileName(testName, "")
		testConfig = `
		<seelog type="sync">
			<outputs>
				<outputs>
					<file path="` + testLogFileName + `"/>
				</outputs>
				<outputs>
					<file path="` + testLogFileName + `"/>
				</outputs>
			</outputs>
		</seelog>
		`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Errors #26"
		testConfig = `
		<seelog type="sync">
			<outputs>
				<conn net="tcp" addr=":8888" reconnectonmsg="true1" />
			</outputs>
		</seelog>`
		parserTests = append(parserTests, parserTest{testName, testConfig, nil, true, nil})

		testName = "Buffered writer same formatid override"
		testLogFileName = getTestFileName(testName, "")
		testConfig = `
		<seelog type="sync">
			<outputs>
				<buffered size="100500" flushperiod="100" formatid="testFormat">
					<rollingfile namemode="prefix" type="date" filename="` + testLogFileName + `" datepattern="2006-01-02T15:04:05Z07:00" formatid="testFormat"/>
				</buffered>
			</outputs>
			<formats>
				<format id="testFormat" format="%Level %Msg %File 123" />
			</formats>
		</seelog>`
		testExpected = new(configForParsing)
		testExpected.Constraints, _ = NewMinMaxConstraints(TraceLvl, CriticalLvl)
		testExpected.Exceptions = nil
		testrollingFileWriterTime, _ = NewRollingFileWriterTime(testLogFileName, rollingArchiveNone, "", 0, "2006-01-02T15:04:05Z07:00", rollingIntervalDaily, rollingNameModePrefix)
		testbufferedWriter, _ = NewBufferedWriter(testrollingFileWriterTime, 100500, 100)
		testFormat, _ = NewFormatter("%Level %Msg %File 123")
		formattedWriter, _ = NewFormattedWriter(testbufferedWriter, testFormat)
		testHeadSplitter, _ = NewSplitDispatcher(DefaultFormatter, []interface{}{formattedWriter})
		testExpected.LogType = syncloggerTypeFromString
		testExpected.RootDispatcher = testHeadSplitter
		parserTests = append(parserTests, parserTest{testName, testConfig, testExpected, false, nil})

	}

	return parserTests
}

// Temporary solution: compare by string identity. Not the best solution in
// terms of performance, but a valid one in terms of comparison, because
// every seelog dispatcher/receiver must have a valid String() func
// that fully represents its internal parameters.
func configsAreEqual(conf1 *configForParsing, conf2 interface{}) bool {
	if conf1 == nil {
		return conf2 == nil
	}
	if conf2 == nil {
		return conf1 == nil
	}

	// configForParsing, ok := conf2 //.(*configForParsing)
	// if !ok {
	// 	return false
	// }

	return fmt.Sprintf("%v", conf1) == fmt.Sprintf("%v", conf2) //configForParsing)
}

func testLogFileFilter(fn string) bool {
	return ".log" == filepath.Ext(fn)
}

func cleanupAfterCfgTest(t *testing.T) {
	toDel, err := getDirFilePaths(".", testLogFileFilter, true)
	if nil != err {
		t.Fatal("Cannot list files in test directory!")
	}

	for _, p := range toDel {
		err = tryRemoveFile(p)
		if nil != err {
			t.Errorf("cannot remove file %s in test directory: %s", p, err.Error())
		}
	}
}

func parseTest(test parserTest, t *testing.T) {
	conf, err := configFromReaderWithConfig(strings.NewReader(test.config), test.parserConfig)
	if /*err != nil &&*/ conf != nil && conf.RootDispatcher != nil {
		defer func() {
			if err = conf.RootDispatcher.Close(); err != nil {
				t.Errorf("\n----ERROR while closing root dispatcher in %s test: %s", test.testName, err)
			}
		}()
	}

	if (err != nil) != test.errorExpected {
		t.Errorf("\n----ERROR in %s:\nConfig: %s\n* Expected error:%t. Got error: %t\n",
			test.testName, test.config, test.errorExpected, (err != nil))
		if err != nil {
			t.Logf("%s\n", err.Error())
		}
		return
	}

	if err == nil && !configsAreEqual(conf, test.expected) {
		t.Errorf("\n----ERROR in %s:\nConfig: %s\n* Expected: %v. \n* Got: %v\n",
			test.testName, test.config, test.expected, conf)
	}
}

func TestParser(t *testing.T) {
	defer cleanupAfterCfgTest(t)

	for _, test := range getParserTests() {
		parseTest(test, t)
	}
}
