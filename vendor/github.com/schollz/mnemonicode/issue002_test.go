package mnemonicode_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"bitbucket.org/dchapes/mnemonicode"
)

func TestIssue002(t *testing.T) {
	buf := &bytes.Buffer{}
	// Code from:
	const issue = `https://bitbucket.org/dchapes/mnemonicode/issues/2`

	config := mnemonicode.NewDefaultConfig()
	config.GroupsPerLine = 1
	config.LineSuffix = "\n"
	config.GroupSeparator = "\n"
	config.WordPadding = 0
	config.WordsPerGroup = 1
	config.WordSeparator = "\n"
	src := strings.NewReader("abcdefgh")
	r := mnemonicode.NewEncodeReader(src, config)
	//io.Copy(os.Stdout, r)
	io.Copy(buf, r)

	// Note, in the issue the expected trailing newline is missing.
	const expected = ` bogart
atlas
safari
airport
cabaret
shock
`
	if s := buf.String(); s != expected {
		t.Errorf("%v\n\tgave %q\n\twant%q", issue, s, expected)
	}
}
