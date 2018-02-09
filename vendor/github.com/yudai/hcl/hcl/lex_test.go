package hcl

import (
	"io/ioutil"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLex(t *testing.T) {
	cases := []struct {
		Input  string
		Output []int
	}{
		{
			"comment.hcl",
			[]int{IDENTIFIER, EQUAL, STRING, lexEOF},
		},
		{
			"comment_single.hcl",
			[]int{lexEOF},
		},
		{
			"complex_key.hcl",
			[]int{IDENTIFIER, EQUAL, STRING, lexEOF},
		},
		{
			"multiple.hcl",
			[]int{
				IDENTIFIER, EQUAL, STRING,
				IDENTIFIER, EQUAL, NUMBER,
				lexEOF,
			},
		},
		{
			"list.hcl",
			[]int{
				IDENTIFIER, EQUAL, LEFTBRACKET,
				NUMBER, COMMA, NUMBER, COMMA, STRING, COMMA, BOOL,
				RIGHTBRACKET, lexEOF,
			},
		},
		{
			"old.hcl",
			[]int{IDENTIFIER, EQUAL, LEFTBRACE, STRING, lexEOF},
		},
		{
			"structure_basic.hcl",
			[]int{
				IDENTIFIER, LEFTBRACE,
				IDENTIFIER, EQUAL, NUMBER,
				STRING, EQUAL, NUMBER,
				STRING, EQUAL, NUMBER,
				RIGHTBRACE, lexEOF,
			},
		},
		{
			"structure.hcl",
			[]int{
				IDENTIFIER, IDENTIFIER, STRING, LEFTBRACE,
				IDENTIFIER, EQUAL, NUMBER,
				IDENTIFIER, EQUAL, STRING,
				RIGHTBRACE, lexEOF,
			},
		},
		{
			"array_comment.hcl",
			[]int{
				IDENTIFIER, EQUAL, LEFTBRACKET,
				STRING, COMMA,
				STRING, COMMA,
				RIGHTBRACKET, lexEOF,
			},
		},
		{
			"null.hcl",
			[]int{
				IDENTIFIER, EQUAL, NULL,
				IDENTIFIER, EQUAL, LEFTBRACKET, NUMBER, COMMA, NULL, COMMA, NUMBER, RIGHTBRACKET,
				IDENTIFIER, LEFTBRACE, IDENTIFIER, EQUAL, NULL, RIGHTBRACE,
				lexEOF,
			},
		},
	}

	for _, tc := range cases {
		d, err := ioutil.ReadFile(filepath.Join(fixtureDir, tc.Input))
		if err != nil {
			t.Fatalf("err: %s", err)
		}

		l := &hclLex{Input: string(d)}
		var actual []int
		for {
			token := l.Lex(new(hclSymType))
			actual = append(actual, token)

			if token == lexEOF {
				break
			}

			if len(actual) > 500 {
				t.Fatalf("Input:%s\n\nExausted.", tc.Input)
			}
		}

		if !reflect.DeepEqual(actual, tc.Output) {
			t.Fatalf(
				"Input: %s\n\nBad: %#v\n\nExpected: %#v",
				tc.Input, actual, tc.Output)
		}
	}
}
