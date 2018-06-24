package json

import (
	"bytes"
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf8"
)

//go:generate go tool yacc -p "json" parse.y

// This marks the end of the lexer
const lexEOF = 0

// The parser uses the type <prefix>Lex as a lexer.  It must provide
// the methods Lex(*<prefix>SymType) int and Error(string).
type jsonLex struct {
	Input string

	pos       int
	width     int
	col, line int
	err       error
}

// The parser calls this method to get each new token.
func (x *jsonLex) Lex(yylval *jsonSymType) int {
	for {
		c := x.next()
		if c == lexEOF {
			return lexEOF
		}

		// Ignore all whitespace except a newline which we handle
		// specially later.
		if unicode.IsSpace(c) {
			continue
		}

		// If it is a number, lex the number
		if c >= '0' && c <= '9' {
			x.backup()
			return x.lexNumber(yylval)
		}

		switch c {
		case 'e':
			fallthrough
		case 'E':
			switch x.next() {
			case '+':
				return EPLUS
			case '-':
				return EMINUS
			default:
				x.backup()
				return EPLUS
			}
		case '.':
			return PERIOD
		case '-':
			return MINUS
		case ':':
			return COLON
		case ',':
			return COMMA
		case '[':
			return LEFTBRACKET
		case ']':
			return RIGHTBRACKET
		case '{':
			return LEFTBRACE
		case '}':
			return RIGHTBRACE
		case '"':
			return x.lexString(yylval)
		default:
			x.backup()
			return x.lexId(yylval)
		}
	}
}

// lexId lexes an identifier
func (x *jsonLex) lexId(yylval *jsonSymType) int {
	var b bytes.Buffer
	first := true
	for {
		c := x.next()
		if c == lexEOF {
			break
		}

		if !unicode.IsDigit(c) && !unicode.IsLetter(c) && c != '_' && c != '-' {
			x.backup()

			if first {
				x.createErr("Invalid identifier")
				return lexEOF
			}

			break
		}

		first = false
		if _, err := b.WriteRune(c); err != nil {
			return lexEOF
		}
	}

	switch v := b.String(); v {
	case "true":
		return TRUE
	case "false":
		return FALSE
	case "null":
		return NULL
	default:
		x.createErr(fmt.Sprintf("Invalid identifier: %s", v))
		return lexEOF
	}
}

// lexNumber lexes out a number
func (x *jsonLex) lexNumber(yylval *jsonSymType) int {
	var b bytes.Buffer
	gotPeriod := false
	for {
		c := x.next()
		if c == lexEOF {
			break
		}

		if c == '.' {
			if gotPeriod {
				x.backup()
				break
			}

			gotPeriod = true
		} else if c < '0' || c > '9' {
			x.backup()
			break
		}

		if _, err := b.WriteRune(c); err != nil {
			x.createErr(fmt.Sprintf("Internal error: %s", err))
			return lexEOF
		}
	}

	if !gotPeriod {
		v, err := strconv.ParseInt(b.String(), 0, 0)
		if err != nil {
			x.createErr(fmt.Sprintf("Expected number: %s", err))
			return lexEOF
		}

		yylval.num = int(v)
		return NUMBER
	}

	f, err := strconv.ParseFloat(b.String(), 64)
	if err != nil {
		x.createErr(fmt.Sprintf("Expected float: %s", err))
		return lexEOF
	}

	yylval.f = float64(f)
	return FLOAT
}

// lexString extracts a string from the input
func (x *jsonLex) lexString(yylval *jsonSymType) int {
	var b bytes.Buffer
	for {
		c := x.next()
		if c == lexEOF {
			break
		}

		// String end
		if c == '"' {
			break
		}

		// If we're escaping a quote, then escape the quote
		if c == '\\' {
			n := x.next()
			switch n {
			case '"':
				c = n
			case 'n':
				c = '\n'
			case '\\':
				c = n
			default:
				x.backup()
			}
		}

		if _, err := b.WriteRune(c); err != nil {
			return lexEOF
		}
	}

	yylval.str = b.String()
	return STRING
}

// Return the next rune for the lexer.
func (x *jsonLex) next() rune {
	if int(x.pos) >= len(x.Input) {
		x.width = 0
		return lexEOF
	}

	r, w := utf8.DecodeRuneInString(x.Input[x.pos:])
	x.width = w
	x.pos += x.width

	x.col += 1
	if x.line == 0 {
		x.line = 1
	}
	if r == '\n' {
		x.line += 1
		x.col = 0
	}

	return r
}

// peek returns but does not consume the next rune in the input
func (x *jsonLex) peek() rune {
	r := x.next()
	x.backup()
	return r
}

// backup steps back one rune. Can only be called once per next.
func (x *jsonLex) backup() {
	x.col -= 1
	x.pos -= x.width
}

// createErr records the given error
func (x *jsonLex) createErr(msg string) {
	x.err = fmt.Errorf("Line %d, column %d: %s", x.line, x.col, msg)
}

// The parser calls this method on a parse error.
func (x *jsonLex) Error(s string) {
	x.createErr(s)
}
