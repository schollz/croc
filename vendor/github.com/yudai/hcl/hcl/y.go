//line parse.y:4
package hcl

import __yyfmt__ "fmt"

//line parse.y:4
import (
	"fmt"
	"strconv"
)

//line parse.y:13
type hclSymType struct {
	yys     int
	b       bool
	f       float64
	num     int
	str     string
	obj     *Object
	objlist []*Object
}

const BOOL = 57346
const FLOAT = 57347
const NUMBER = 57348
const COMMA = 57349
const IDENTIFIER = 57350
const EQUAL = 57351
const NEWLINE = 57352
const STRING = 57353
const MINUS = 57354
const LEFTBRACE = 57355
const RIGHTBRACE = 57356
const LEFTBRACKET = 57357
const RIGHTBRACKET = 57358
const PERIOD = 57359
const EPLUS = 57360
const EMINUS = 57361
const NULL = 57362

var hclToknames = [...]string{
	"$end",
	"error",
	"$unk",
	"BOOL",
	"FLOAT",
	"NUMBER",
	"COMMA",
	"IDENTIFIER",
	"EQUAL",
	"NEWLINE",
	"STRING",
	"MINUS",
	"LEFTBRACE",
	"RIGHTBRACE",
	"LEFTBRACKET",
	"RIGHTBRACKET",
	"PERIOD",
	"EPLUS",
	"EMINUS",
	"NULL",
}
var hclStatenames = [...]string{}

const hclEofCode = 1
const hclErrCode = 2
const hclMaxDepth = 200

//line parse.y:281

//line yacctab:1
var hclExca = [...]int{
	-1, 1,
	1, -1,
	-2, 0,
	-1, 6,
	9, 7,
	-2, 18,
	-1, 7,
	9, 8,
	-2, 19,
}

const hclNprod = 39
const hclPrivate = 57344

var hclTokenNames []string
var hclStates []string

const hclLast = 69

var hclAct = [...]int{

	36, 3, 22, 30, 9, 17, 27, 26, 31, 32,
	45, 23, 19, 25, 13, 10, 24, 39, 27, 26,
	6, 18, 47, 7, 38, 25, 43, 33, 41, 48,
	9, 46, 44, 40, 39, 27, 26, 42, 5, 1,
	14, 38, 25, 15, 2, 13, 35, 12, 49, 6,
	40, 4, 7, 27, 26, 29, 11, 37, 28, 6,
	25, 8, 7, 34, 21, 0, 0, 20, 16,
}
var hclPact = [...]int{

	51, -1000, 51, -1000, 6, -1000, -1000, -1000, 32, -1000,
	1, -1000, -1000, 41, -1000, -1000, -1000, -1000, -1000, -1000,
	-1000, -1000, -10, -10, 30, 48, -1000, -1000, 12, -1000,
	-1000, 26, 4, -1000, 15, -1000, -1000, -1000, -1000, -1000,
	-1000, -1000, -1000, -1000, -1000, -1000, -1000, 13, -1000, -1000,
}
var hclPgo = [...]int{

	0, 11, 2, 64, 63, 44, 38, 57, 56, 1,
	0, 61, 3, 51, 39,
}
var hclR1 = [...]int{

	0, 14, 14, 5, 5, 8, 8, 13, 13, 9,
	9, 9, 9, 9, 9, 9, 6, 6, 11, 11,
	3, 3, 3, 4, 4, 10, 10, 10, 10, 7,
	7, 7, 7, 2, 2, 1, 1, 12, 12,
}
var hclR2 = [...]int{

	0, 0, 1, 1, 2, 3, 2, 1, 1, 3,
	3, 3, 3, 3, 3, 1, 2, 2, 1, 1,
	3, 4, 2, 1, 3, 1, 1, 1, 1, 1,
	1, 2, 2, 2, 1, 2, 1, 2, 2,
}
var hclChk = [...]int{

	-1000, -14, -5, -9, -13, -6, 8, 11, -11, -9,
	9, -8, -6, 13, 8, 11, -7, 4, 20, 11,
	-8, -3, -2, -1, 15, 12, 6, 5, -5, 14,
	-12, 18, 19, -12, -4, 16, -10, -7, 11, 4,
	20, -2, -1, 14, 6, 6, 16, 7, 16, -10,
}
var hclDef = [...]int{

	1, -2, 2, 3, 0, 15, -2, -2, 0, 4,
	0, 16, 17, 0, 18, 19, 9, 10, 11, 12,
	13, 14, 29, 30, 0, 0, 34, 36, 0, 6,
	31, 0, 0, 32, 0, 22, 23, 25, 26, 27,
	28, 33, 35, 5, 37, 38, 20, 0, 21, 24,
}
var hclTok1 = [...]int{

	1,
}
var hclTok2 = [...]int{

	2, 3, 4, 5, 6, 7, 8, 9, 10, 11,
	12, 13, 14, 15, 16, 17, 18, 19, 20,
}
var hclTok3 = [...]int{
	0,
}

var hclErrorMessages = [...]struct {
	state int
	token int
	msg   string
}{}

//line yaccpar:1

/*	parser for yacc output	*/

var (
	hclDebug        = 0
	hclErrorVerbose = false
)

type hclLexer interface {
	Lex(lval *hclSymType) int
	Error(s string)
}

type hclParser interface {
	Parse(hclLexer) int
	Lookahead() int
}

type hclParserImpl struct {
	lookahead func() int
}

func (p *hclParserImpl) Lookahead() int {
	return p.lookahead()
}

func hclNewParser() hclParser {
	p := &hclParserImpl{
		lookahead: func() int { return -1 },
	}
	return p
}

const hclFlag = -1000

func hclTokname(c int) string {
	if c >= 1 && c-1 < len(hclToknames) {
		if hclToknames[c-1] != "" {
			return hclToknames[c-1]
		}
	}
	return __yyfmt__.Sprintf("tok-%v", c)
}

func hclStatname(s int) string {
	if s >= 0 && s < len(hclStatenames) {
		if hclStatenames[s] != "" {
			return hclStatenames[s]
		}
	}
	return __yyfmt__.Sprintf("state-%v", s)
}

func hclErrorMessage(state, lookAhead int) string {
	const TOKSTART = 4

	if !hclErrorVerbose {
		return "syntax error"
	}

	for _, e := range hclErrorMessages {
		if e.state == state && e.token == lookAhead {
			return "syntax error: " + e.msg
		}
	}

	res := "syntax error: unexpected " + hclTokname(lookAhead)

	// To match Bison, suggest at most four expected tokens.
	expected := make([]int, 0, 4)

	// Look for shiftable tokens.
	base := hclPact[state]
	for tok := TOKSTART; tok-1 < len(hclToknames); tok++ {
		if n := base + tok; n >= 0 && n < hclLast && hclChk[hclAct[n]] == tok {
			if len(expected) == cap(expected) {
				return res
			}
			expected = append(expected, tok)
		}
	}

	if hclDef[state] == -2 {
		i := 0
		for hclExca[i] != -1 || hclExca[i+1] != state {
			i += 2
		}

		// Look for tokens that we accept or reduce.
		for i += 2; hclExca[i] >= 0; i += 2 {
			tok := hclExca[i]
			if tok < TOKSTART || hclExca[i+1] == 0 {
				continue
			}
			if len(expected) == cap(expected) {
				return res
			}
			expected = append(expected, tok)
		}

		// If the default action is to accept or reduce, give up.
		if hclExca[i+1] != 0 {
			return res
		}
	}

	for i, tok := range expected {
		if i == 0 {
			res += ", expecting "
		} else {
			res += " or "
		}
		res += hclTokname(tok)
	}
	return res
}

func hcllex1(lex hclLexer, lval *hclSymType) (char, token int) {
	token = 0
	char = lex.Lex(lval)
	if char <= 0 {
		token = hclTok1[0]
		goto out
	}
	if char < len(hclTok1) {
		token = hclTok1[char]
		goto out
	}
	if char >= hclPrivate {
		if char < hclPrivate+len(hclTok2) {
			token = hclTok2[char-hclPrivate]
			goto out
		}
	}
	for i := 0; i < len(hclTok3); i += 2 {
		token = hclTok3[i+0]
		if token == char {
			token = hclTok3[i+1]
			goto out
		}
	}

out:
	if token == 0 {
		token = hclTok2[1] /* unknown char */
	}
	if hclDebug >= 3 {
		__yyfmt__.Printf("lex %s(%d)\n", hclTokname(token), uint(char))
	}
	return char, token
}

func hclParse(hcllex hclLexer) int {
	return hclNewParser().Parse(hcllex)
}

func (hclrcvr *hclParserImpl) Parse(hcllex hclLexer) int {
	var hcln int
	var hcllval hclSymType
	var hclVAL hclSymType
	var hclDollar []hclSymType
	_ = hclDollar // silence set and not used
	hclS := make([]hclSymType, hclMaxDepth)

	Nerrs := 0   /* number of errors */
	Errflag := 0 /* error recovery flag */
	hclstate := 0
	hclchar := -1
	hcltoken := -1 // hclchar translated into internal numbering
	hclrcvr.lookahead = func() int { return hclchar }
	defer func() {
		// Make sure we report no lookahead when not parsing.
		hclstate = -1
		hclchar = -1
		hcltoken = -1
	}()
	hclp := -1
	goto hclstack

ret0:
	return 0

ret1:
	return 1

hclstack:
	/* put a state and value onto the stack */
	if hclDebug >= 4 {
		__yyfmt__.Printf("char %v in %v\n", hclTokname(hcltoken), hclStatname(hclstate))
	}

	hclp++
	if hclp >= len(hclS) {
		nyys := make([]hclSymType, len(hclS)*2)
		copy(nyys, hclS)
		hclS = nyys
	}
	hclS[hclp] = hclVAL
	hclS[hclp].yys = hclstate

hclnewstate:
	hcln = hclPact[hclstate]
	if hcln <= hclFlag {
		goto hcldefault /* simple state */
	}
	if hclchar < 0 {
		hclchar, hcltoken = hcllex1(hcllex, &hcllval)
	}
	hcln += hcltoken
	if hcln < 0 || hcln >= hclLast {
		goto hcldefault
	}
	hcln = hclAct[hcln]
	if hclChk[hcln] == hcltoken { /* valid shift */
		hclchar = -1
		hcltoken = -1
		hclVAL = hcllval
		hclstate = hcln
		if Errflag > 0 {
			Errflag--
		}
		goto hclstack
	}

hcldefault:
	/* default state action */
	hcln = hclDef[hclstate]
	if hcln == -2 {
		if hclchar < 0 {
			hclchar, hcltoken = hcllex1(hcllex, &hcllval)
		}

		/* look through exception table */
		xi := 0
		for {
			if hclExca[xi+0] == -1 && hclExca[xi+1] == hclstate {
				break
			}
			xi += 2
		}
		for xi += 2; ; xi += 2 {
			hcln = hclExca[xi+0]
			if hcln < 0 || hcln == hcltoken {
				break
			}
		}
		hcln = hclExca[xi+1]
		if hcln < 0 {
			goto ret0
		}
	}
	if hcln == 0 {
		/* error ... attempt to resume parsing */
		switch Errflag {
		case 0: /* brand new error */
			hcllex.Error(hclErrorMessage(hclstate, hcltoken))
			Nerrs++
			if hclDebug >= 1 {
				__yyfmt__.Printf("%s", hclStatname(hclstate))
				__yyfmt__.Printf(" saw %s\n", hclTokname(hcltoken))
			}
			fallthrough

		case 1, 2: /* incompletely recovered error ... try again */
			Errflag = 3

			/* find a state where "error" is a legal shift action */
			for hclp >= 0 {
				hcln = hclPact[hclS[hclp].yys] + hclErrCode
				if hcln >= 0 && hcln < hclLast {
					hclstate = hclAct[hcln] /* simulate a shift of "error" */
					if hclChk[hclstate] == hclErrCode {
						goto hclstack
					}
				}

				/* the current p has no shift on "error", pop stack */
				if hclDebug >= 2 {
					__yyfmt__.Printf("error recovery pops state %d\n", hclS[hclp].yys)
				}
				hclp--
			}
			/* there is no state on the stack with an error shift ... abort */
			goto ret1

		case 3: /* no shift yet; clobber input char */
			if hclDebug >= 2 {
				__yyfmt__.Printf("error recovery discards %s\n", hclTokname(hcltoken))
			}
			if hcltoken == hclEofCode {
				goto ret1
			}
			hclchar = -1
			hcltoken = -1
			goto hclnewstate /* try again in the same state */
		}
	}

	/* reduction by production hcln */
	if hclDebug >= 2 {
		__yyfmt__.Printf("reduce %v in:\n\t%v\n", hcln, hclStatname(hclstate))
	}

	hclnt := hcln
	hclpt := hclp
	_ = hclpt // guard against "declared and not used"

	hclp -= hclR2[hcln]
	// hclp is now the index of $0. Perform the default action. Iff the
	// reduced production is ε, $1 is possibly out of range.
	if hclp+1 >= len(hclS) {
		nyys := make([]hclSymType, len(hclS)*2)
		copy(nyys, hclS)
		hclS = nyys
	}
	hclVAL = hclS[hclp+1]

	/* consult goto table to find next state */
	hcln = hclR1[hcln]
	hclg := hclPgo[hcln]
	hclj := hclg + hclS[hclp].yys + 1

	if hclj >= hclLast {
		hclstate = hclAct[hclg]
	} else {
		hclstate = hclAct[hclj]
		if hclChk[hclstate] != -hcln {
			hclstate = hclAct[hclg]
		}
	}
	// dummy call; replaced with literal code
	switch hclnt {

	case 1:
		hclDollar = hclS[hclpt-0 : hclpt+1]
		//line parse.y:40
		{
			hclResult = &Object{Type: ValueTypeObject}
		}
	case 2:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:44
		{
			hclResult = &Object{
				Type:  ValueTypeObject,
				Value: ObjectList(hclDollar[1].objlist).Flat(),
			}
		}
	case 3:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:53
		{
			hclVAL.objlist = []*Object{hclDollar[1].obj}
		}
	case 4:
		hclDollar = hclS[hclpt-2 : hclpt+1]
		//line parse.y:57
		{
			hclVAL.objlist = append(hclDollar[1].objlist, hclDollar[2].obj)
		}
	case 5:
		hclDollar = hclS[hclpt-3 : hclpt+1]
		//line parse.y:63
		{
			hclVAL.obj = &Object{
				Type:  ValueTypeObject,
				Value: ObjectList(hclDollar[2].objlist).Flat(),
			}
		}
	case 6:
		hclDollar = hclS[hclpt-2 : hclpt+1]
		//line parse.y:70
		{
			hclVAL.obj = &Object{
				Type: ValueTypeObject,
			}
		}
	case 7:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:78
		{
			hclVAL.str = hclDollar[1].str
		}
	case 8:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:82
		{
			hclVAL.str = hclDollar[1].str
		}
	case 9:
		hclDollar = hclS[hclpt-3 : hclpt+1]
		//line parse.y:88
		{
			hclVAL.obj = hclDollar[3].obj
			hclVAL.obj.Key = hclDollar[1].str
		}
	case 10:
		hclDollar = hclS[hclpt-3 : hclpt+1]
		//line parse.y:93
		{
			hclVAL.obj = &Object{
				Key:   hclDollar[1].str,
				Type:  ValueTypeBool,
				Value: hclDollar[3].b,
			}
		}
	case 11:
		hclDollar = hclS[hclpt-3 : hclpt+1]
		//line parse.y:101
		{
			hclVAL.obj = &Object{
				Key:  hclDollar[1].str,
				Type: ValueTypeNil,
			}
		}
	case 12:
		hclDollar = hclS[hclpt-3 : hclpt+1]
		//line parse.y:108
		{
			hclVAL.obj = &Object{
				Key:   hclDollar[1].str,
				Type:  ValueTypeString,
				Value: hclDollar[3].str,
			}
		}
	case 13:
		hclDollar = hclS[hclpt-3 : hclpt+1]
		//line parse.y:116
		{
			hclDollar[3].obj.Key = hclDollar[1].str
			hclVAL.obj = hclDollar[3].obj
		}
	case 14:
		hclDollar = hclS[hclpt-3 : hclpt+1]
		//line parse.y:121
		{
			hclVAL.obj = &Object{
				Key:   hclDollar[1].str,
				Type:  ValueTypeList,
				Value: hclDollar[3].objlist,
			}
		}
	case 15:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:129
		{
			hclVAL.obj = hclDollar[1].obj
		}
	case 16:
		hclDollar = hclS[hclpt-2 : hclpt+1]
		//line parse.y:135
		{
			hclDollar[2].obj.Key = hclDollar[1].str
			hclVAL.obj = hclDollar[2].obj
		}
	case 17:
		hclDollar = hclS[hclpt-2 : hclpt+1]
		//line parse.y:140
		{
			hclVAL.obj = &Object{
				Key:   hclDollar[1].str,
				Type:  ValueTypeObject,
				Value: []*Object{hclDollar[2].obj},
			}
		}
	case 18:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:150
		{
			hclVAL.str = hclDollar[1].str
		}
	case 19:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:154
		{
			hclVAL.str = hclDollar[1].str
		}
	case 20:
		hclDollar = hclS[hclpt-3 : hclpt+1]
		//line parse.y:160
		{
			hclVAL.objlist = hclDollar[2].objlist
		}
	case 21:
		hclDollar = hclS[hclpt-4 : hclpt+1]
		//line parse.y:164
		{
			hclVAL.objlist = hclDollar[2].objlist
		}
	case 22:
		hclDollar = hclS[hclpt-2 : hclpt+1]
		//line parse.y:168
		{
			hclVAL.objlist = nil
		}
	case 23:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:174
		{
			hclVAL.objlist = []*Object{hclDollar[1].obj}
		}
	case 24:
		hclDollar = hclS[hclpt-3 : hclpt+1]
		//line parse.y:178
		{
			hclVAL.objlist = append(hclDollar[1].objlist, hclDollar[3].obj)
		}
	case 25:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:184
		{
			hclVAL.obj = hclDollar[1].obj
		}
	case 26:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:188
		{
			hclVAL.obj = &Object{
				Type:  ValueTypeString,
				Value: hclDollar[1].str,
			}
		}
	case 27:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:195
		{
			hclVAL.obj = &Object{
				Type:  ValueTypeBool,
				Value: hclDollar[1].b,
			}
		}
	case 28:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:202
		{
			hclVAL.obj = &Object{
				Type: ValueTypeNil,
			}
		}
	case 29:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:211
		{
			hclVAL.obj = &Object{
				Type:  ValueTypeInt,
				Value: hclDollar[1].num,
			}
		}
	case 30:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:218
		{
			hclVAL.obj = &Object{
				Type:  ValueTypeFloat,
				Value: hclDollar[1].f,
			}
		}
	case 31:
		hclDollar = hclS[hclpt-2 : hclpt+1]
		//line parse.y:225
		{
			fs := fmt.Sprintf("%d%s", hclDollar[1].num, hclDollar[2].str)
			f, err := strconv.ParseFloat(fs, 64)
			if err != nil {
				panic(err)
			}

			hclVAL.obj = &Object{
				Type:  ValueTypeFloat,
				Value: f,
			}
		}
	case 32:
		hclDollar = hclS[hclpt-2 : hclpt+1]
		//line parse.y:238
		{
			fs := fmt.Sprintf("%f%s", hclDollar[1].f, hclDollar[2].str)
			f, err := strconv.ParseFloat(fs, 64)
			if err != nil {
				panic(err)
			}

			hclVAL.obj = &Object{
				Type:  ValueTypeFloat,
				Value: f,
			}
		}
	case 33:
		hclDollar = hclS[hclpt-2 : hclpt+1]
		//line parse.y:253
		{
			hclVAL.num = hclDollar[2].num * -1
		}
	case 34:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:257
		{
			hclVAL.num = hclDollar[1].num
		}
	case 35:
		hclDollar = hclS[hclpt-2 : hclpt+1]
		//line parse.y:263
		{
			hclVAL.f = hclDollar[2].f * -1
		}
	case 36:
		hclDollar = hclS[hclpt-1 : hclpt+1]
		//line parse.y:267
		{
			hclVAL.f = hclDollar[1].f
		}
	case 37:
		hclDollar = hclS[hclpt-2 : hclpt+1]
		//line parse.y:273
		{
			hclVAL.str = "e" + strconv.FormatInt(int64(hclDollar[2].num), 10)
		}
	case 38:
		hclDollar = hclS[hclpt-2 : hclpt+1]
		//line parse.y:277
		{
			hclVAL.str = "e-" + strconv.FormatInt(int64(hclDollar[2].num), 10)
		}
	}
	goto hclstack /* stack new state and value */
}
