//line parse.y:3
package json

import __yyfmt__ "fmt"

//line parse.y:5
import (
	"fmt"
	"strconv"

	"github.com/yudai/hcl/hcl"
)

//line parse.y:15
type jsonSymType struct {
	yys     int
	f       float64
	num     int
	str     string
	obj     *hcl.Object
	objlist []*hcl.Object
}

const FLOAT = 57346
const NUMBER = 57347
const COLON = 57348
const COMMA = 57349
const IDENTIFIER = 57350
const EQUAL = 57351
const NEWLINE = 57352
const STRING = 57353
const LEFTBRACE = 57354
const RIGHTBRACE = 57355
const LEFTBRACKET = 57356
const RIGHTBRACKET = 57357
const TRUE = 57358
const FALSE = 57359
const NULL = 57360
const MINUS = 57361
const PERIOD = 57362
const EPLUS = 57363
const EMINUS = 57364

var jsonToknames = [...]string{
	"$end",
	"error",
	"$unk",
	"FLOAT",
	"NUMBER",
	"COLON",
	"COMMA",
	"IDENTIFIER",
	"EQUAL",
	"NEWLINE",
	"STRING",
	"LEFTBRACE",
	"RIGHTBRACE",
	"LEFTBRACKET",
	"RIGHTBRACKET",
	"TRUE",
	"FALSE",
	"NULL",
	"MINUS",
	"PERIOD",
	"EPLUS",
	"EMINUS",
}
var jsonStatenames = [...]string{}

const jsonEofCode = 1
const jsonErrCode = 2
const jsonMaxDepth = 200

//line parse.y:210

//line yacctab:1
var jsonExca = [...]int{
	-1, 1,
	1, -1,
	-2, 0,
}

const jsonNprod = 28
const jsonPrivate = 57344

var jsonTokenNames []string
var jsonStates []string

const jsonLast = 53

var jsonAct = [...]int{

	12, 25, 24, 3, 20, 27, 28, 7, 13, 3,
	21, 22, 30, 17, 18, 19, 23, 25, 24, 26,
	25, 24, 36, 32, 13, 3, 10, 22, 33, 17,
	18, 19, 23, 35, 34, 23, 38, 9, 7, 39,
	5, 29, 6, 8, 37, 15, 2, 1, 4, 31,
	16, 14, 11,
}
var jsonPact = [...]int{

	-9, -1000, -1000, 27, 30, -1000, -1000, 20, -1000, -4,
	13, -1000, -1000, -1000, -1000, -1000, -1000, -1000, -1000, -1000,
	-16, -16, -3, 16, -1000, -1000, -1000, 28, 17, -1000,
	-1000, 29, -1000, -1000, -1000, -1000, -1000, -1000, 13, -1000,
}
var jsonPgo = [...]int{

	0, 10, 4, 51, 45, 42, 0, 50, 49, 48,
	19, 47,
}
var jsonR1 = [...]int{

	0, 11, 4, 4, 9, 9, 5, 6, 6, 6,
	6, 6, 6, 6, 7, 7, 8, 8, 3, 3,
	3, 3, 2, 2, 1, 1, 10, 10,
}
var jsonR2 = [...]int{

	0, 1, 3, 2, 1, 3, 3, 1, 1, 1,
	1, 1, 1, 1, 2, 3, 1, 3, 1, 1,
	2, 2, 2, 1, 2, 1, 2, 2,
}
var jsonChk = [...]int{

	-1000, -11, -4, 12, -9, 13, -5, 11, 13, 7,
	6, -5, -6, 11, -3, -4, -7, 16, 17, 18,
	-2, -1, 14, 19, 5, 4, -10, 21, 22, -10,
	15, -8, -6, -2, -1, 5, 5, 15, 7, -6,
}
var jsonDef = [...]int{

	0, -2, 1, 0, 0, 3, 4, 0, 2, 0,
	0, 5, 6, 7, 8, 9, 10, 11, 12, 13,
	18, 19, 0, 0, 23, 25, 20, 0, 0, 21,
	14, 0, 16, 22, 24, 26, 27, 15, 0, 17,
}
var jsonTok1 = [...]int{

	1,
}
var jsonTok2 = [...]int{

	2, 3, 4, 5, 6, 7, 8, 9, 10, 11,
	12, 13, 14, 15, 16, 17, 18, 19, 20, 21,
	22,
}
var jsonTok3 = [...]int{
	0,
}

var jsonErrorMessages = [...]struct {
	state int
	token int
	msg   string
}{}

//line yaccpar:1

/*	parser for yacc output	*/

var (
	jsonDebug        = 0
	jsonErrorVerbose = false
)

type jsonLexer interface {
	Lex(lval *jsonSymType) int
	Error(s string)
}

type jsonParser interface {
	Parse(jsonLexer) int
	Lookahead() int
}

type jsonParserImpl struct {
	lookahead func() int
}

func (p *jsonParserImpl) Lookahead() int {
	return p.lookahead()
}

func jsonNewParser() jsonParser {
	p := &jsonParserImpl{
		lookahead: func() int { return -1 },
	}
	return p
}

const jsonFlag = -1000

func jsonTokname(c int) string {
	if c >= 1 && c-1 < len(jsonToknames) {
		if jsonToknames[c-1] != "" {
			return jsonToknames[c-1]
		}
	}
	return __yyfmt__.Sprintf("tok-%v", c)
}

func jsonStatname(s int) string {
	if s >= 0 && s < len(jsonStatenames) {
		if jsonStatenames[s] != "" {
			return jsonStatenames[s]
		}
	}
	return __yyfmt__.Sprintf("state-%v", s)
}

func jsonErrorMessage(state, lookAhead int) string {
	const TOKSTART = 4

	if !jsonErrorVerbose {
		return "syntax error"
	}

	for _, e := range jsonErrorMessages {
		if e.state == state && e.token == lookAhead {
			return "syntax error: " + e.msg
		}
	}

	res := "syntax error: unexpected " + jsonTokname(lookAhead)

	// To match Bison, suggest at most four expected tokens.
	expected := make([]int, 0, 4)

	// Look for shiftable tokens.
	base := jsonPact[state]
	for tok := TOKSTART; tok-1 < len(jsonToknames); tok++ {
		if n := base + tok; n >= 0 && n < jsonLast && jsonChk[jsonAct[n]] == tok {
			if len(expected) == cap(expected) {
				return res
			}
			expected = append(expected, tok)
		}
	}

	if jsonDef[state] == -2 {
		i := 0
		for jsonExca[i] != -1 || jsonExca[i+1] != state {
			i += 2
		}

		// Look for tokens that we accept or reduce.
		for i += 2; jsonExca[i] >= 0; i += 2 {
			tok := jsonExca[i]
			if tok < TOKSTART || jsonExca[i+1] == 0 {
				continue
			}
			if len(expected) == cap(expected) {
				return res
			}
			expected = append(expected, tok)
		}

		// If the default action is to accept or reduce, give up.
		if jsonExca[i+1] != 0 {
			return res
		}
	}

	for i, tok := range expected {
		if i == 0 {
			res += ", expecting "
		} else {
			res += " or "
		}
		res += jsonTokname(tok)
	}
	return res
}

func jsonlex1(lex jsonLexer, lval *jsonSymType) (char, token int) {
	token = 0
	char = lex.Lex(lval)
	if char <= 0 {
		token = jsonTok1[0]
		goto out
	}
	if char < len(jsonTok1) {
		token = jsonTok1[char]
		goto out
	}
	if char >= jsonPrivate {
		if char < jsonPrivate+len(jsonTok2) {
			token = jsonTok2[char-jsonPrivate]
			goto out
		}
	}
	for i := 0; i < len(jsonTok3); i += 2 {
		token = jsonTok3[i+0]
		if token == char {
			token = jsonTok3[i+1]
			goto out
		}
	}

out:
	if token == 0 {
		token = jsonTok2[1] /* unknown char */
	}
	if jsonDebug >= 3 {
		__yyfmt__.Printf("lex %s(%d)\n", jsonTokname(token), uint(char))
	}
	return char, token
}

func jsonParse(jsonlex jsonLexer) int {
	return jsonNewParser().Parse(jsonlex)
}

func (jsonrcvr *jsonParserImpl) Parse(jsonlex jsonLexer) int {
	var jsonn int
	var jsonlval jsonSymType
	var jsonVAL jsonSymType
	var jsonDollar []jsonSymType
	_ = jsonDollar // silence set and not used
	jsonS := make([]jsonSymType, jsonMaxDepth)

	Nerrs := 0   /* number of errors */
	Errflag := 0 /* error recovery flag */
	jsonstate := 0
	jsonchar := -1
	jsontoken := -1 // jsonchar translated into internal numbering
	jsonrcvr.lookahead = func() int { return jsonchar }
	defer func() {
		// Make sure we report no lookahead when not parsing.
		jsonstate = -1
		jsonchar = -1
		jsontoken = -1
	}()
	jsonp := -1
	goto jsonstack

ret0:
	return 0

ret1:
	return 1

jsonstack:
	/* put a state and value onto the stack */
	if jsonDebug >= 4 {
		__yyfmt__.Printf("char %v in %v\n", jsonTokname(jsontoken), jsonStatname(jsonstate))
	}

	jsonp++
	if jsonp >= len(jsonS) {
		nyys := make([]jsonSymType, len(jsonS)*2)
		copy(nyys, jsonS)
		jsonS = nyys
	}
	jsonS[jsonp] = jsonVAL
	jsonS[jsonp].yys = jsonstate

jsonnewstate:
	jsonn = jsonPact[jsonstate]
	if jsonn <= jsonFlag {
		goto jsondefault /* simple state */
	}
	if jsonchar < 0 {
		jsonchar, jsontoken = jsonlex1(jsonlex, &jsonlval)
	}
	jsonn += jsontoken
	if jsonn < 0 || jsonn >= jsonLast {
		goto jsondefault
	}
	jsonn = jsonAct[jsonn]
	if jsonChk[jsonn] == jsontoken { /* valid shift */
		jsonchar = -1
		jsontoken = -1
		jsonVAL = jsonlval
		jsonstate = jsonn
		if Errflag > 0 {
			Errflag--
		}
		goto jsonstack
	}

jsondefault:
	/* default state action */
	jsonn = jsonDef[jsonstate]
	if jsonn == -2 {
		if jsonchar < 0 {
			jsonchar, jsontoken = jsonlex1(jsonlex, &jsonlval)
		}

		/* look through exception table */
		xi := 0
		for {
			if jsonExca[xi+0] == -1 && jsonExca[xi+1] == jsonstate {
				break
			}
			xi += 2
		}
		for xi += 2; ; xi += 2 {
			jsonn = jsonExca[xi+0]
			if jsonn < 0 || jsonn == jsontoken {
				break
			}
		}
		jsonn = jsonExca[xi+1]
		if jsonn < 0 {
			goto ret0
		}
	}
	if jsonn == 0 {
		/* error ... attempt to resume parsing */
		switch Errflag {
		case 0: /* brand new error */
			jsonlex.Error(jsonErrorMessage(jsonstate, jsontoken))
			Nerrs++
			if jsonDebug >= 1 {
				__yyfmt__.Printf("%s", jsonStatname(jsonstate))
				__yyfmt__.Printf(" saw %s\n", jsonTokname(jsontoken))
			}
			fallthrough

		case 1, 2: /* incompletely recovered error ... try again */
			Errflag = 3

			/* find a state where "error" is a legal shift action */
			for jsonp >= 0 {
				jsonn = jsonPact[jsonS[jsonp].yys] + jsonErrCode
				if jsonn >= 0 && jsonn < jsonLast {
					jsonstate = jsonAct[jsonn] /* simulate a shift of "error" */
					if jsonChk[jsonstate] == jsonErrCode {
						goto jsonstack
					}
				}

				/* the current p has no shift on "error", pop stack */
				if jsonDebug >= 2 {
					__yyfmt__.Printf("error recovery pops state %d\n", jsonS[jsonp].yys)
				}
				jsonp--
			}
			/* there is no state on the stack with an error shift ... abort */
			goto ret1

		case 3: /* no shift yet; clobber input char */
			if jsonDebug >= 2 {
				__yyfmt__.Printf("error recovery discards %s\n", jsonTokname(jsontoken))
			}
			if jsontoken == jsonEofCode {
				goto ret1
			}
			jsonchar = -1
			jsontoken = -1
			goto jsonnewstate /* try again in the same state */
		}
	}

	/* reduction by production jsonn */
	if jsonDebug >= 2 {
		__yyfmt__.Printf("reduce %v in:\n\t%v\n", jsonn, jsonStatname(jsonstate))
	}

	jsonnt := jsonn
	jsonpt := jsonp
	_ = jsonpt // guard against "declared and not used"

	jsonp -= jsonR2[jsonn]
	// jsonp is now the index of $0. Perform the default action. Iff the
	// reduced production is ε, $1 is possibly out of range.
	if jsonp+1 >= len(jsonS) {
		nyys := make([]jsonSymType, len(jsonS)*2)
		copy(nyys, jsonS)
		jsonS = nyys
	}
	jsonVAL = jsonS[jsonp+1]

	/* consult goto table to find next state */
	jsonn = jsonR1[jsonn]
	jsong := jsonPgo[jsonn]
	jsonj := jsong + jsonS[jsonp].yys + 1

	if jsonj >= jsonLast {
		jsonstate = jsonAct[jsong]
	} else {
		jsonstate = jsonAct[jsonj]
		if jsonChk[jsonstate] != -jsonn {
			jsonstate = jsonAct[jsong]
		}
	}
	// dummy call; replaced with literal code
	switch jsonnt {

	case 1:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:39
		{
			jsonResult = jsonDollar[1].obj
		}
	case 2:
		jsonDollar = jsonS[jsonpt-3 : jsonpt+1]
		//line parse.y:45
		{
			jsonVAL.obj = &hcl.Object{
				Type:  hcl.ValueTypeObject,
				Value: hcl.ObjectList(jsonDollar[2].objlist).Flat(),
			}
		}
	case 3:
		jsonDollar = jsonS[jsonpt-2 : jsonpt+1]
		//line parse.y:52
		{
			jsonVAL.obj = &hcl.Object{Type: hcl.ValueTypeObject}
		}
	case 4:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:58
		{
			jsonVAL.objlist = []*hcl.Object{jsonDollar[1].obj}
		}
	case 5:
		jsonDollar = jsonS[jsonpt-3 : jsonpt+1]
		//line parse.y:62
		{
			jsonVAL.objlist = append(jsonDollar[1].objlist, jsonDollar[3].obj)
		}
	case 6:
		jsonDollar = jsonS[jsonpt-3 : jsonpt+1]
		//line parse.y:68
		{
			jsonDollar[3].obj.Key = jsonDollar[1].str
			jsonVAL.obj = jsonDollar[3].obj
		}
	case 7:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:75
		{
			jsonVAL.obj = &hcl.Object{
				Type:  hcl.ValueTypeString,
				Value: jsonDollar[1].str,
			}
		}
	case 8:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:82
		{
			jsonVAL.obj = jsonDollar[1].obj
		}
	case 9:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:86
		{
			jsonVAL.obj = jsonDollar[1].obj
		}
	case 10:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:90
		{
			jsonVAL.obj = &hcl.Object{
				Type:  hcl.ValueTypeList,
				Value: jsonDollar[1].objlist,
			}
		}
	case 11:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:97
		{
			jsonVAL.obj = &hcl.Object{
				Type:  hcl.ValueTypeBool,
				Value: true,
			}
		}
	case 12:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:104
		{
			jsonVAL.obj = &hcl.Object{
				Type:  hcl.ValueTypeBool,
				Value: false,
			}
		}
	case 13:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:111
		{
			jsonVAL.obj = &hcl.Object{
				Type:  hcl.ValueTypeNil,
				Value: nil,
			}
		}
	case 14:
		jsonDollar = jsonS[jsonpt-2 : jsonpt+1]
		//line parse.y:120
		{
			jsonVAL.objlist = nil
		}
	case 15:
		jsonDollar = jsonS[jsonpt-3 : jsonpt+1]
		//line parse.y:124
		{
			jsonVAL.objlist = jsonDollar[2].objlist
		}
	case 16:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:130
		{
			jsonVAL.objlist = []*hcl.Object{jsonDollar[1].obj}
		}
	case 17:
		jsonDollar = jsonS[jsonpt-3 : jsonpt+1]
		//line parse.y:134
		{
			jsonVAL.objlist = append(jsonDollar[1].objlist, jsonDollar[3].obj)
		}
	case 18:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:140
		{
			jsonVAL.obj = &hcl.Object{
				Type:  hcl.ValueTypeInt,
				Value: jsonDollar[1].num,
			}
		}
	case 19:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:147
		{
			jsonVAL.obj = &hcl.Object{
				Type:  hcl.ValueTypeFloat,
				Value: jsonDollar[1].f,
			}
		}
	case 20:
		jsonDollar = jsonS[jsonpt-2 : jsonpt+1]
		//line parse.y:154
		{
			fs := fmt.Sprintf("%d%s", jsonDollar[1].num, jsonDollar[2].str)
			f, err := strconv.ParseFloat(fs, 64)
			if err != nil {
				panic(err)
			}

			jsonVAL.obj = &hcl.Object{
				Type:  hcl.ValueTypeFloat,
				Value: f,
			}
		}
	case 21:
		jsonDollar = jsonS[jsonpt-2 : jsonpt+1]
		//line parse.y:167
		{
			fs := fmt.Sprintf("%f%s", jsonDollar[1].f, jsonDollar[2].str)
			f, err := strconv.ParseFloat(fs, 64)
			if err != nil {
				panic(err)
			}

			jsonVAL.obj = &hcl.Object{
				Type:  hcl.ValueTypeFloat,
				Value: f,
			}
		}
	case 22:
		jsonDollar = jsonS[jsonpt-2 : jsonpt+1]
		//line parse.y:182
		{
			jsonVAL.num = jsonDollar[2].num * -1
		}
	case 23:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:186
		{
			jsonVAL.num = jsonDollar[1].num
		}
	case 24:
		jsonDollar = jsonS[jsonpt-2 : jsonpt+1]
		//line parse.y:192
		{
			jsonVAL.f = jsonDollar[2].f * -1
		}
	case 25:
		jsonDollar = jsonS[jsonpt-1 : jsonpt+1]
		//line parse.y:196
		{
			jsonVAL.f = jsonDollar[1].f
		}
	case 26:
		jsonDollar = jsonS[jsonpt-2 : jsonpt+1]
		//line parse.y:202
		{
			jsonVAL.str = "e" + strconv.FormatInt(int64(jsonDollar[2].num), 10)
		}
	case 27:
		jsonDollar = jsonS[jsonpt-2 : jsonpt+1]
		//line parse.y:206
		{
			jsonVAL.str = "e-" + strconv.FormatInt(int64(jsonDollar[2].num), 10)
		}
	}
	goto jsonstack /* stack new state and value */
}
