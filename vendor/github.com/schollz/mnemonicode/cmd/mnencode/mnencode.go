package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path"
	"strconv"

	"golang.org/x/text/transform"

	"bitbucket.org/dchapes/mnemonicode"
)

type quoted string

func (q quoted) Get() interface{} { return string(q) }
func (q quoted) String() string   { return strconv.Quote(string(q)) }
func (q *quoted) Set(s string) (err error) {
	if s, err = strconv.Unquote(`"` + s + `"`); err == nil {
		*q = quoted(s)
	}
	return
}

type quotedRune rune

func (qr quotedRune) Get() interface{} { return rune(qr) }
func (qr quotedRune) String() string   { return strconv.QuoteRune(rune(qr)) }
func (qr *quotedRune) Set(s string) error {
	r, _, x, err := strconv.UnquoteChar(s, 0)
	if err != nil {
		return err
	}
	if x != "" {
		return fmt.Errorf("more than a single rune")
	}
	*qr = quotedRune(r)
	return nil
}

func main() {
	log.SetFlags(0)
	log.SetPrefix(path.Base(os.Args[0]) + ": ")
	vlog := log.New(os.Stderr, log.Prefix(), log.Flags())

	config := mnemonicode.NewDefaultConfig()
	prefix := quoted(config.LinePrefix)
	suffix := quoted(config.LineSuffix)
	wordsep := quoted(config.WordSeparator)
	groupsep := quoted(config.GroupSeparator)
	pad := quotedRune(config.WordPadding)

	flag.Var(&prefix, "prefix", "prefix each line with `string`")
	flag.Var(&suffix, "suffix", "suffix each line with `string`")
	flag.Var(&wordsep, "word", "separate each word with `wsep`")
	flag.Var(&groupsep, "group", "separate each word group with `gsep`")
	words := flag.Uint("words", config.WordsPerGroup, "words per group")
	groups := flag.Uint("groups", config.GroupsPerLine, "groups per line")
	nopad := flag.Bool("nopad", false, "do not pad words")
	flag.Var(&pad, "pad", "pad shorter words with `rune`")
	hexin := flag.Bool("x", false, "hex input")
	verbose := flag.Bool("v", false, "verbose")

	flag.Parse()
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}

	if !*verbose {
		vlog.SetOutput(ioutil.Discard)
	}

	config.LinePrefix = prefix.Get().(string)
	config.LineSuffix = suffix.Get().(string)
	config.GroupSeparator = groupsep.Get().(string)
	config.WordSeparator = wordsep.Get().(string)
	config.WordPadding = pad.Get().(rune)
	if *words > 0 {
		config.WordsPerGroup = *words
	}
	if *groups > 0 {
		config.GroupsPerLine = *groups
	}
	if *nopad {
		config.WordPadding = 0
	}

	vlog.Println("Wordlist ver", mnemonicode.WordListVersion)

	input := io.Reader(os.Stdin)
	if *hexin {
		input = transform.NewReader(input, new(hexinput))
	}

	var n int64
	var err error
	if true {
		enc := mnemonicode.NewEncoder(os.Stdout, config)
		n, err = io.Copy(enc, input)
		if err != nil {
			log.Fatal(err)
		}
		err = enc.Close()
	} else {
		r := mnemonicode.NewEncodeReader(input, config)
		n, err = io.Copy(os.Stdout, r)
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println()
	vlog.Println("bytes encoded:", n)
}
