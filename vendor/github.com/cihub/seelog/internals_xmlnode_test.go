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
	//"fmt"
	"reflect"
)

var testEnv *testing.T

/*func TestWrapper(t *testing.T) {
	testEnv = t

	s := "<a d='a'><g m='a'></g><g h='t' j='kk'></g></a>"
	reader := strings.NewReader(s)
	config, err := unmarshalConfig(reader)
	if err != nil {
		testEnv.Error(err)
		return
	}

	printXML(config, 0)
}

func printXML(node *xmlNode, level int) {
	indent := strings.Repeat("\t", level)
	fmt.Print(indent + node.name)
	for key, value := range node.attributes {
		fmt.Print(" " + key + "/" + value)
	}
	fmt.Println()

	for _, child := range node.children {
		printXML(child, level+1)
	}
}*/

var xmlNodeTests []xmlNodeTest

type xmlNodeTest struct {
	testName      string
	inputXML      string
	expected      interface{}
	errorExpected bool
}

func getXMLTests() []xmlNodeTest {
	if xmlNodeTests == nil {
		xmlNodeTests = make([]xmlNodeTest, 0)

		testName := "Simple test"
		testXML := `<a></a>`
		testExpected := newNode()
		testExpected.name = "a"
		xmlNodeTests = append(xmlNodeTests, xmlNodeTest{testName, testXML, testExpected, false})

		testName = "Multiline test"
		testXML =
			`
<a>
</a>
`
		testExpected = newNode()
		testExpected.name = "a"
		xmlNodeTests = append(xmlNodeTests, xmlNodeTest{testName, testXML, testExpected, false})

		testName = "Multiline test #2"
		testXML =
			`


<a>

</a>

`
		testExpected = newNode()
		testExpected.name = "a"
		xmlNodeTests = append(xmlNodeTests, xmlNodeTest{testName, testXML, testExpected, false})

		testName = "Incorrect names"
		testXML = `< a     ><      /a >`
		xmlNodeTests = append(xmlNodeTests, xmlNodeTest{testName, testXML, nil, true})

		testName = "Comments"
		testXML =
			`<!-- <abcdef/> -->
<a> <!-- <!--12345-->
</a>
`
		testExpected = newNode()
		testExpected.name = "a"
		xmlNodeTests = append(xmlNodeTests, xmlNodeTest{testName, testXML, testExpected, false})

		testName = "Multiple roots"
		testXML = `<a></a><b></b>`
		xmlNodeTests = append(xmlNodeTests, xmlNodeTest{testName, testXML, nil, true})

		testName = "Multiple roots + incorrect xml"
		testXML = `<a></a><b>`
		xmlNodeTests = append(xmlNodeTests, xmlNodeTest{testName, testXML, nil, true})

		testName = "Some unicode and data"
		testXML = `<俄语>данные</俄语>`
		testExpected = newNode()
		testExpected.name = "俄语"
		testExpected.value = "данные"
		xmlNodeTests = append(xmlNodeTests, xmlNodeTest{testName, testXML, testExpected, false})

		testName = "Values and children"
		testXML = `<俄语>данные<and_a_child></and_a_child></俄语>`
		testExpected = newNode()
		testExpected.name = "俄语"
		testExpected.value = "данные"
		child := newNode()
		child.name = "and_a_child"
		testExpected.children = append(testExpected.children, child)
		xmlNodeTests = append(xmlNodeTests, xmlNodeTest{testName, testXML, testExpected, false})

		testName = "Just children"
		testXML = `<俄语><and_a_child></and_a_child></俄语>`
		testExpected = newNode()
		testExpected.name = "俄语"
		child = newNode()
		child.name = "and_a_child"
		testExpected.children = append(testExpected.children, child)
		xmlNodeTests = append(xmlNodeTests, xmlNodeTest{testName, testXML, testExpected, false})

		testName = "Mixed test"
		testXML = `<俄语 a="1" b="2.13" c="abc"><child abc="bca"/><child abc="def"></child></俄语>`
		testExpected = newNode()
		testExpected.name = "俄语"
		testExpected.attributes["a"] = "1"
		testExpected.attributes["b"] = "2.13"
		testExpected.attributes["c"] = "abc"
		child = newNode()
		child.name = "child"
		child.attributes["abc"] = "bca"
		testExpected.children = append(testExpected.children, child)
		child = newNode()
		child.name = "child"
		child.attributes["abc"] = "def"
		testExpected.children = append(testExpected.children, child)
		xmlNodeTests = append(xmlNodeTests, xmlNodeTest{testName, testXML, testExpected, false})
	}

	return xmlNodeTests
}

func TestXmlNode(t *testing.T) {

	for _, test := range getXMLTests() {

		reader := strings.NewReader(test.inputXML)
		parsedXML, err := unmarshalConfig(reader)

		if (err != nil) != test.errorExpected {
			t.Errorf("\n%s:\nXML input: %s\nExpected error:%t. Got error: %t\n", test.testName,
				test.inputXML, test.errorExpected, (err != nil))
			if err != nil {
				t.Logf("%s\n", err.Error())
			}
			continue
		}

		if err == nil && !reflect.DeepEqual(parsedXML, test.expected) {
			t.Errorf("\n%s:\nXML input: %s\nExpected: %s. \nGot: %s\n", test.testName,
				test.inputXML, test.expected, parsedXML)
		}
	}
}
