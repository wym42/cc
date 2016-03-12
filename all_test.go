// Copyright 2016 The CC Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cc

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"go/scanner"
	"go/token"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"unicode"

	"github.com/cznic/golex/lex"
	"github.com/cznic/xc"
)

func printStack() { debug.PrintStack() }

func caller(s string, va ...interface{}) {
	if s == "" {
		s = strings.Repeat("%v ", len(va))
	}
	_, fn, fl, _ := runtime.Caller(2)
	fmt.Fprintf(os.Stderr, "caller: %s:%d: ", path.Base(fn), fl)
	fmt.Fprintf(os.Stderr, s, va...)
	fmt.Fprintln(os.Stderr)
	_, fn, fl, _ = runtime.Caller(1)
	fmt.Fprintf(os.Stderr, "\tcallee: %s:%d: ", path.Base(fn), fl)
	fmt.Fprintln(os.Stderr)
	os.Stderr.Sync()
}

func dbg(s string, va ...interface{}) {
	if s == "" {
		s = strings.Repeat("%v ", len(va))
	}
	_, fn, fl, _ := runtime.Caller(1)
	fmt.Fprintf(os.Stderr, "dbg %s:%d: ", path.Base(fn), fl)
	fmt.Fprintf(os.Stderr, s, va...)
	fmt.Fprintln(os.Stderr)
	os.Stderr.Sync()
}

func TODO(...interface{}) string {
	_, fn, fl, _ := runtime.Caller(1)
	return fmt.Sprintf("TODO: %s:%d:\n", path.Base(fn), fl)
}

func use(...interface{}) {}

// ============================================================================

var (
	o1        = flag.String("1", "", "Single file argument of TestPPParse1.")
	oAST      = flag.Bool("ast", false, "Show AST.")
	oFailFast = flag.Bool("ff", false, "crash on first reported error (in some tests.)")
	oLong     = flag.Bool("long", false, "Enable long tests. (false)")
	oRE       = flag.String("re", "", "Regexp filter.")
	oTmp      = flag.Bool("tmp", false, "keep certain temp files.")

	includes = []string{}

	predefinedMacros = `
#define __STDC_HOSTED__ 1
#define __STDC_VERSION__ 199901L
#define __STDC__ 1

#define __MODEL64

void __GO__(char *s, ...);
`
	sysIncludes = []string{}

	testTweaks = &tweaks{
		enableDefineOmitCommaBeforeDDD: true,
		enableDlrInIdentifiers:         true,
		enableEmptyDefine:              true,
		enableUndefExtraTokens:         true,
	}
)

func newTestReport() *xc.Report {
	r := xc.NewReport()
	r.ErrLimit = -1
	if *oFailFast {
		r.PanicOnError = true
	}
	return r
}

func init() {
	isTesting = true
	log.SetFlags(log.Llongfile)
	flag.IntVar(&yyDebug, "yydebug", 0, "")
	flag.BoolVar(&isGenerating, "generating", false, "go generate is executing (false).")
	flag.BoolVar(&debugTypeStrings, "xtypes", false, "add debug info to type strings")
}

func newTestModel() *Model {
	return &Model{ // 64
		Items: map[Kind]ModelItem{
			Ptr:               {8, 8, 8, nil},
			UintPtr:           {8, 8, 8, nil},
			Void:              {0, 1, 1, nil},
			Char:              {1, 1, 1, nil},
			SChar:             {1, 1, 1, nil},
			UChar:             {1, 1, 1, nil},
			Short:             {2, 2, 2, nil},
			UShort:            {2, 2, 2, nil},
			Int:               {4, 4, 4, nil},
			UInt:              {4, 4, 4, nil},
			Long:              {8, 8, 8, nil},
			ULong:             {8, 8, 8, nil},
			LongLong:          {8, 8, 8, nil},
			ULongLong:         {8, 8, 8, nil},
			Float:             {4, 4, 4, nil},
			Double:            {8, 8, 8, nil},
			LongDouble:        {8, 8, 8, nil},
			Bool:              {1, 1, 1, nil},
			FloatComplex:      {8, 8, 8, nil},
			DoubleComplex:     {16, 16, 16, nil},
			LongDoubleComplex: {16, 16, 16, nil},
		},
	}
}

func printError(w io.Writer, pref string, err error) {
	switch x := err.(type) {
	case scanner.ErrorList:
		for _, v := range x {
			fmt.Fprintf(w, "%s%v\n", pref, v)
		}
	default:
		fmt.Fprintf(w, "%s%v\n", pref, err)
	}
}

func errString(err error) string {
	var b bytes.Buffer
	printError(&b, "", err)
	return b.String()
}

func testUCNTable(t *testing.T, tab []rune, fOk, fOther func(rune) bool, fcategory func(rune) bool, tag string) {
	m := map[rune]struct{}{}
	for i := 0; i < len(tab); i += 2 {
		l, h := tab[i], tab[i+1]
		if h == 0 {
			h = l
		}
		for r := rune(l); r <= rune(h); r++ {
			m[r] = struct{}{}
		}
	}
	for r := rune(0); r < 0xffff; r++ {
		_, ok := m[r]
		if g, e := fOk(r), ok; g != e {
			t.Errorf("%#04x %v %v", r, g, e)
		}

		if ok {
			if g, e := fOther(r), false; g != e {
				t.Errorf("%#04x %v %v", r, g, e)
			}
		}
	}
}

func TestUCNDigitsTable(t *testing.T) {
	tab := []rune{
		0x0660, 0x0669, 0x06F0, 0x06F9, 0x0966, 0x096F, 0x09E6, 0x09EF, 0x0A66, 0x0A6F,
		0x0AE6, 0x0AEF, 0x0B66, 0x0B6F, 0x0BE7, 0x0BEF, 0x0C66, 0x0C6F, 0x0CE6, 0x0CEF,
		0x0D66, 0x0D6F, 0x0E50, 0x0E59, 0x0ED0, 0x0ED9, 0x0F20, 0x0F33,
	}
	testUCNTable(t, tab, isUCNDigit, isUCNNonDigit, unicode.IsDigit, "unicode.IsDigit")
}

func TestUCNNonDigitsTable(t *testing.T) {
	tab := []rune{
		0x00AA, 0x0000, 0x00B5, 0x0000, 0x00B7, 0x0000, 0x00BA, 0x0000, 0x00C0, 0x00D6,
		0x00D8, 0x00F6, 0x00F8, 0x01F5, 0x01FA, 0x0217, 0x0250, 0x02A8, 0x02B0, 0x02B8,
		0x02BB, 0x0000, 0x02BD, 0x02C1, 0x02D0, 0x02D1, 0x02E0, 0x02E4, 0x037A, 0x0000,
		0x0386, 0x0000, 0x0388, 0x038A, 0x038C, 0x0000, 0x038E, 0x03A1, 0x03A3, 0x03CE,
		0x03D0, 0x03D6, 0x03DA, 0x0000, 0x03DC, 0x0000, 0x03DE, 0x0000, 0x03E0, 0x0000,
		0x03E2, 0x03F3, 0x0401, 0x040C, 0x040E, 0x044F, 0x0451, 0x045C, 0x045E, 0x0481,
		0x0490, 0x04C4, 0x04C7, 0x04C8, 0x04CB, 0x04CC, 0x04D0, 0x04EB, 0x04EE, 0x04F5,
		0x04F8, 0x04F9, 0x0531, 0x0556, 0x0559, 0x0000, 0x0561, 0x0587, 0x05B0, 0x05B9,
		0x05F0, 0x05F2, 0x0621, 0x063A, 0x0640, 0x0652, 0x0670, 0x06B7, 0x06BA, 0x06BE,
		0x06C0, 0x06CE, 0x06D0, 0x06DC, 0x06E5, 0x06E8, 0x06EA, 0x06ED, 0x0901, 0x0903,
		0x0905, 0x0939, 0x093D, 0x0000, 0x093E, 0x094D, 0x0950, 0x0952, 0x0958, 0x0963,
		0x0981, 0x0983, 0x0985, 0x098C, 0x098F, 0x0990, 0x0993, 0x09A8, 0x09AA, 0x09B0,
		0x09B2, 0x0000, 0x09B6, 0x09B9, 0x09BE, 0x09C4, 0x09C7, 0x09C8, 0x09CB, 0x09CD,
		0x09DC, 0x09DD, 0x09DF, 0x09E3, 0x09F0, 0x09F1, 0x0A02, 0x0000, 0x0A05, 0x0A0A,
		0x0A0F, 0x0A10, 0x0A13, 0x0A28, 0x0A2A, 0x0A30, 0x0A32, 0x0A33, 0x0A35, 0x0A36,
		0x0A38, 0x0A39, 0x0A3E, 0x0A42, 0x0A47, 0x0A48, 0x0A4B, 0x0A4D, 0x0A59, 0x0A5C,
		0x0A5E, 0x0000, 0x0A74, 0x0000, 0x0A81, 0x0A83, 0x0A85, 0x0A8B, 0x0A8D, 0x0000,
		0x0A8F, 0x0A91, 0x0A93, 0x0AA8, 0x0AAA, 0x0AB0, 0x0AB2, 0x0AB3, 0x0AB5, 0x0AB9,
		0x0ABD, 0x0AC5, 0x0AC7, 0x0AC9, 0x0ACB, 0x0ACD, 0x0AD0, 0x0000, 0x0AE0, 0x0000,
		0x0B01, 0x0B03, 0x0B05, 0x0B0C, 0x0B0F, 0x0B10, 0x0B13, 0x0B28, 0x0B2A, 0x0B30,
		0x0B32, 0x0B33, 0x0B36, 0x0B39, 0x0B3D, 0x0000, 0x0B3E, 0x0B43, 0x0B47, 0x0B48,
		0x0B4B, 0x0B4D, 0x0B5C, 0x0B5D, 0x0B5F, 0x0B61, 0x0B82, 0x0B83, 0x0B85, 0x0B8A,
		0x0B8E, 0x0B90, 0x0B92, 0x0B95, 0x0B99, 0x0B9A, 0x0B9C, 0x0000, 0x0B9E, 0x0B9F,
		0x0BA3, 0x0BA4, 0x0BA8, 0x0BAA, 0x0BAE, 0x0BB5, 0x0BB7, 0x0BB9, 0x0BBE, 0x0BC2,
		0x0BC6, 0x0BC8, 0x0BCA, 0x0BCD, 0x0C01, 0x0C03, 0x0C05, 0x0C0C, 0x0C0E, 0x0C10,
		0x0C12, 0x0C28, 0x0C2A, 0x0C33, 0x0C35, 0x0C39, 0x0C3E, 0x0C44, 0x0C46, 0x0C48,
		0x0C4A, 0x0C4D, 0x0C60, 0x0C61, 0x0C82, 0x0C83, 0x0C85, 0x0C8C, 0x0C8E, 0x0C90,
		0x0C92, 0x0CA8, 0x0CAA, 0x0CB3, 0x0CB5, 0x0CB9, 0x0CBE, 0x0CC4, 0x0CC6, 0x0CC8,
		0x0CCA, 0x0CCD, 0x0CDE, 0x0000, 0x0CE0, 0x0CE1, 0x0D02, 0x0D03, 0x0D05, 0x0D0C,
		0x0D0E, 0x0D10, 0x0D12, 0x0D28, 0x0D2A, 0x0D39, 0x0D3E, 0x0D43, 0x0D46, 0x0D48,
		0x0D4A, 0x0D4D, 0x0D60, 0x0D61, 0x0E01, 0x0E3A,

		// In [0], Annex D, Thai [0x0E40, 0x0E5B] overlaps with digits
		// [0x0E50, 0x0E59]. Exclude them.
		0x0E40, 0x0E4F,
		0x0E5A, 0x0E5B,

		0x0E81, 0x0E82,
		0x0E84, 0x0000, 0x0E87, 0x0E88, 0x0E8A, 0x0000, 0x0E8D, 0x0000, 0x0E94, 0x0E97,
		0x0E99, 0x0E9F, 0x0EA1, 0x0EA3, 0x0EA5, 0x0000, 0x0EA7, 0x0000, 0x0EAA, 0x0EAB,
		0x0EAD, 0x0EAE, 0x0EB0, 0x0EB9, 0x0EBB, 0x0EBD, 0x0EC0, 0x0EC4, 0x0EC6, 0x0000,
		0x0EC8, 0x0ECD, 0x0EDC, 0x0EDD, 0x0F00, 0x0000, 0x0F18, 0x0F19, 0x0F35, 0x0000,
		0x0F37, 0x0000, 0x0F39, 0x0000, 0x0F3E, 0x0F47, 0x0F49, 0x0F69, 0x0F71, 0x0F84,
		0x0F86, 0x0F8B, 0x0F90, 0x0F95, 0x0F97, 0x0000, 0x0F99, 0x0FAD, 0x0FB1, 0x0FB7,
		0x0FB9, 0x0000, 0x10A0, 0x10C5, 0x10D0, 0x10F6, 0x1E00, 0x1E9B, 0x1EA0, 0x1EF9,
		0x1F00, 0x1F15, 0x1F18, 0x1F1D, 0x1F20, 0x1F45, 0x1F48, 0x1F4D, 0x1F50, 0x1F57,
		0x1F59, 0x0000, 0x1F5B, 0x0000, 0x1F5D, 0x0000, 0x1F5F, 0x1F7D, 0x1F80, 0x1FB4,
		0x1FB6, 0x1FBC, 0x1FBE, 0x0000, 0x1FC2, 0x1FC4, 0x1FC6, 0x1FCC, 0x1FD0, 0x1FD3,
		0x1FD6, 0x1FDB, 0x1FE0, 0x1FEC, 0x1FF2, 0x1FF4, 0x1FF6, 0x1FFC, 0x203F, 0x2040,
		0x207F, 0x0000, 0x2102, 0x0000, 0x2107, 0x0000, 0x210A, 0x2113, 0x2115, 0x0000,
		0x2118, 0x211D, 0x2124, 0x0000, 0x2126, 0x0000, 0x2128, 0x0000, 0x212A, 0x2131,
		0x2133, 0x2138, 0x2160, 0x2182, 0x3005, 0x3007, 0x3021, 0x3029, 0x3041, 0x3093,
		0x309B, 0x309C, 0x30A1, 0x30F6, 0x30FB, 0x30FC, 0x3105, 0x312C, 0x4E00, 0x9FA5,
		0xAC00, 0xD7A3,
	}
	testUCNTable(t, tab, isUCNNonDigit, isUCNDigit, unicode.IsLetter, "unicode.IsLetter")
}

func charStr(c rune) string {
	return yySymName(int(c))
}

func charsStr(chars []lex.Char, delta token.Pos) (a []string) {
	for _, v := range chars {
		a = append(a, fmt.Sprintf("{%s %d}", charStr(v.Rune), v.Pos()-delta))
	}
	return a
}

type x []struct {
	c   rune
	pos token.Pos
}

type lexerTests []struct {
	src   string
	chars x
}

func testLexer(t *testing.T, newLexer func(i int, src string, report *xc.Report) (*lexer, error), tab lexerTests) {
nextTest:
	for ti, test := range tab {
		//dbg("==== %v", ti)
		report := xc.NewReport()
		lx, err := newLexer(ti, test.src, report)
		if err != nil {
			t.Fatal(err)
		}

		delta := token.Pos(lx.file.Base() - 1)
		var chars []lex.Char
		var c lex.Char
		for i := 0; c.Rune != ccEOF && i < len(test.src)+2; i++ {
			c = lx.scanChar()
			chars = append(chars, c)
		}
		if c.Rune != ccEOF {
			t.Errorf("%d: scanner stall %v", ti, charsStr(chars, delta))
			continue
		}

		if g, e := report.Errors(true), error(nil); g != e {
			t.Errorf("%d: lx.err %v %v %v", ti, g, e, charsStr(chars, delta))
			continue
		}

		if g, e := len(chars), len(test.chars); g != e {
			t.Errorf("%d: len(chars) %v %v %v", ti, g, e, charsStr(chars, delta))
			continue
		}

		for i, c := range chars {
			c = chars[i]
			e := test.chars[i]
			g := c.Rune
			if c.Rune == ccEOF {
				g = -1
			}
			if e := e.c; g != e {
				t.Errorf("%d: c[%d] %v %v %v", ti, i, charStr(g), charStr(e), charsStr(chars, delta))
				continue nextTest
			}

			if g, e := c.Pos()-delta, e.pos; g != e {
				t.Errorf("%d: pos[%d] %v %v %v", ti, i, g, e, charsStr(chars, delta))
				continue nextTest
			}
		}
	}
}

func TestLexer(t *testing.T) {
	testLexer(
		t,
		func(i int, src string, report *xc.Report) (*lexer, error) {
			return newLexer(fmt.Sprintf("TestLexer.%d", i), len(src), strings.NewReader(src), report, testTweaks)
		},
		lexerTests{
			{"", x{{-1, 1}}},
			{"%0", x{{'%', 1}, {INTCONST, 2}, {-1, 3}}},
			{"%:%:", x{{PPPASTE, 1}, {-1, 5}}},
			{"%>", x{{'}', 1}, {-1, 3}}},
			{"0", x{{INTCONST, 1}, {-1, 2}}},
			{"01", x{{INTCONST, 1}, {-1, 3}}},
			{"0??/1\n", x{{INTCONST, 1}, {'?', 2}, {'?', 3}, {'/', 4}, {INTCONST, 5}, {'\n', 6}, {-1, 7}}},
			{"0??/1\n2", x{{INTCONST, 1}, {'?', 2}, {'?', 3}, {'/', 4}, {INTCONST, 5}, {'\n', 6}, {INTCONST, 7}, {-1, 8}}},
			{"0??/\n", x{{INTCONST, 1}, {'?', 2}, {'?', 3}, {'/', 4}, {'\n', 5}, {-1, 6}}},
			{"0??/\n2", x{{INTCONST, 1}, {'?', 2}, {'?', 3}, {'/', 4}, {'\n', 5}, {INTCONST, 6}, {-1, 7}}},
			{"0\\1\n", x{{INTCONST, 1}, {'\\', 2}, {INTCONST, 3}, {'\n', 4}, {-1, 5}}},
			{"0\\1\n2", x{{INTCONST, 1}, {'\\', 2}, {INTCONST, 3}, {'\n', 4}, {INTCONST, 5}, {-1, 6}}},
			{"0\\\n", x{{INTCONST, 1}, {-1, 4}}},
			{"0\\\n2", x{{INTCONST, 1}, {-1, 5}}},
			{"0\x00", x{{INTCONST, 1}, {0, 2}, {-1, 3}}},
			{"0\x001", x{{INTCONST, 1}, {0, 2}, {INTCONST, 3}, {-1, 4}}},
			{":>", x{{']', 1}, {-1, 3}}},
			{"<%", x{{'{', 1}, {-1, 3}}},
			{"<:", x{{'[', 1}, {-1, 3}}},
			{"??!", x{{'?', 1}, {'?', 2}, {'!', 3}, {-1, 4}}},
			{"??!0", x{{'?', 1}, {'?', 2}, {'!', 3}, {INTCONST, 4}, {-1, 5}}},
			{"??!01", x{{'?', 1}, {'?', 2}, {'!', 3}, {INTCONST, 4}, {-1, 6}}},
			{"??!=", x{{'?', 1}, {'?', 2}, {NEQ, 3}, {-1, 5}}},
			{"??'", x{{'?', 1}, {'?', 2}, {'\'', 3}, {-1, 4}}},
			{"??(", x{{'?', 1}, {'?', 2}, {'(', 3}, {-1, 4}}},
			{"??)", x{{'?', 1}, {'?', 2}, {')', 3}, {-1, 4}}},
			{"??-", x{{'?', 1}, {'?', 2}, {'-', 3}, {-1, 4}}},
			{"??/", x{{'?', 1}, {'?', 2}, {'/', 3}, {-1, 4}}},
			{"??/1\n", x{{'?', 1}, {'?', 2}, {'/', 3}, {INTCONST, 4}, {'\n', 5}, {-1, 6}}},
			{"??/1\n2", x{{'?', 1}, {'?', 2}, {'/', 3}, {INTCONST, 4}, {'\n', 5}, {INTCONST, 6}, {-1, 7}}},
			{"??/\n", x{{'?', 1}, {'?', 2}, {'/', 3}, {'\n', 4}, {-1, 5}}},
			{"??/\n2", x{{'?', 1}, {'?', 2}, {'/', 3}, {'\n', 4}, {INTCONST, 5}, {-1, 6}}},
			{"??<", x{{'?', 1}, {'?', 2}, {'<', 3}, {-1, 4}}},
			{"??=??=", x{{'?', 1}, {'?', 2}, {'=', 3}, {'?', 4}, {'?', 5}, {'=', 6}, {-1, 7}}},
			{"??>", x{{'?', 1}, {'?', 2}, {'>', 3}, {-1, 4}}},
			{"???!", x{{'?', 1}, {'?', 2}, {'?', 3}, {'!', 4}, {-1, 5}}},
			{"???!0", x{{'?', 1}, {'?', 2}, {'?', 3}, {'!', 4}, {INTCONST, 5}, {-1, 6}}},
			{"???/\n2", x{{'?', 1}, {'?', 2}, {'?', 3}, {'/', 4}, {'\n', 5}, {INTCONST, 6}, {-1, 7}}},
			{"????!0", x{{'?', 1}, {'?', 2}, {'?', 3}, {'?', 4}, {'!', 5}, {INTCONST, 6}, {-1, 7}}},
			{"???x0", x{{'?', 1}, {'?', 2}, {'?', 3}, {IDENTIFIER, 4}, {-1, 6}}},
			{"???x??!0", x{{'?', 1}, {'?', 2}, {'?', 3}, {IDENTIFIER, 4}, {'?', 5}, {'?', 6}, {'!', 7}, {INTCONST, 8}, {-1, 9}}},
			{"??x0", x{{'?', 1}, {'?', 2}, {IDENTIFIER, 3}, {-1, 5}}},
			{"??x??!0", x{{'?', 1}, {'?', 2}, {IDENTIFIER, 3}, {'?', 4}, {'?', 5}, {'!', 6}, {INTCONST, 7}, {-1, 8}}},
			{"?x0", x{{'?', 1}, {IDENTIFIER, 2}, {-1, 4}}},
			{"?x??!0", x{{'?', 1}, {IDENTIFIER, 2}, {'?', 3}, {'?', 4}, {'!', 5}, {INTCONST, 6}, {-1, 7}}},
			{"@", x{{'@', 1}, {-1, 2}}},
			{"@%", x{{'@', 1}, {'%', 2}, {-1, 3}}},
			{"@%0", x{{'@', 1}, {'%', 2}, {INTCONST, 3}, {-1, 4}}},
			{"@%:", x{{'@', 1}, {'#', 2}, {-1, 4}}},
			{"@%:0", x{{'@', 1}, {'#', 2}, {INTCONST, 4}, {-1, 5}}},
			{"@%:01", x{{'@', 1}, {'#', 2}, {INTCONST, 4}, {-1, 6}}},
			{"@??=", x{{'@', 1}, {'?', 2}, {'?', 3}, {'=', 4}, {-1, 5}}},
			{"\"(a\\\nz", x{{'"', 1}, {'(', 2}, {IDENTIFIER, 3}, {-1, 7}}},
			{"\\1\n", x{{'\\', 1}, {INTCONST, 2}, {'\n', 3}, {-1, 4}}},
			{"\\1\n2", x{{'\\', 1}, {INTCONST, 2}, {'\n', 3}, {INTCONST, 4}, {-1, 5}}},
			{"\\\n", x{{-1, 3}}},
			{"\\\n2", x{{INTCONST, 3}, {-1, 4}}},
			{"\\\r\n", x{{-1, 4}}},
			{"\\\r\n2", x{{INTCONST, 4}, {-1, 5}}},
			{"\r", x{{-1, 2}}},
			{"\r0", x{{INTCONST, 2}, {-1, 3}}},
			{"\r01", x{{INTCONST, 2}, {-1, 4}}},
			{"\x00", x{{0, 1}, {-1, 2}}},
			{"\x000", x{{0, 1}, {INTCONST, 2}, {-1, 3}}},
		},
	)
}

func TestLexer2(t *testing.T) {
	testLexer(
		t,
		func(i int, src string, report *xc.Report) (*lexer, error) {
			tweaks := *testTweaks
			tweaks.enableTrigraphs = true
			return newLexer(fmt.Sprintf("TestLexer.%d", i), len(src), strings.NewReader(src), report, &tweaks)
		},
		lexerTests{
			{"", x{{-1, 1}}},
			{"%0", x{{'%', 1}, {INTCONST, 2}, {-1, 3}}},
			{"%:%:", x{{PPPASTE, 1}, {-1, 5}}},
			{"%>", x{{'}', 1}, {-1, 3}}},
			{"0", x{{INTCONST, 1}, {-1, 2}}},
			{"01", x{{INTCONST, 1}, {-1, 3}}},
			{"0??/1\n", x{{INTCONST, 1}, {'\\', 2}, {INTCONST, 5}, {'\n', 6}, {-1, 7}}},
			{"0??/1\n2", x{{INTCONST, 1}, {'\\', 2}, {INTCONST, 5}, {'\n', 6}, {INTCONST, 7}, {-1, 8}}},
			{"0??/\n", x{{INTCONST, 1}, {-1, 6}}},
			{"0??/\n2", x{{INTCONST, 1}, {-1, 7}}},
			{"0\\1\n", x{{INTCONST, 1}, {'\\', 2}, {INTCONST, 3}, {'\n', 4}, {-1, 5}}},
			{"0\\1\n2", x{{INTCONST, 1}, {'\\', 2}, {INTCONST, 3}, {'\n', 4}, {INTCONST, 5}, {-1, 6}}},
			{"0\\\n", x{{INTCONST, 1}, {-1, 4}}},
			{"0\\\n2", x{{INTCONST, 1}, {-1, 5}}},
			{"0\x00", x{{INTCONST, 1}, {0, 2}, {-1, 3}}},
			{"0\x001", x{{INTCONST, 1}, {0, 2}, {INTCONST, 3}, {-1, 4}}},
			{":>", x{{']', 1}, {-1, 3}}},
			{"<%", x{{'{', 1}, {-1, 3}}},
			{"<:", x{{'[', 1}, {-1, 3}}},
			{"??!", x{{'|', 1}, {-1, 4}}},
			{"??!0", x{{'|', 1}, {INTCONST, 4}, {-1, 5}}},
			{"??!01", x{{'|', 1}, {INTCONST, 4}, {-1, 6}}},
			{"??!=", x{{ORASSIGN, 1}, {-1, 5}}},
			{"??'", x{{'^', 1}, {-1, 4}}},
			{"??(", x{{'[', 1}, {-1, 4}}},
			{"??)", x{{']', 1}, {-1, 4}}},
			{"??-", x{{'~', 1}, {-1, 4}}},
			{"??/", x{{'\\', 1}, {-1, 4}}},
			{"??/1\n", x{{'\\', 1}, {INTCONST, 4}, {'\n', 5}, {-1, 6}}},
			{"??/1\n2", x{{'\\', 1}, {INTCONST, 4}, {'\n', 5}, {INTCONST, 6}, {-1, 7}}},
			{"??/\n", x{{-1, 5}}},
			{"??/\n2", x{{INTCONST, 5}, {-1, 6}}},
			{"??<", x{{'{', 1}, {-1, 4}}},
			{"??=??=", x{{PPPASTE, 1}, {-1, 7}}},
			{"??>", x{{'}', 1}, {-1, 4}}},
			{"???!", x{{'?', 1}, {'|', 2}, {-1, 5}}},
			{"???!0", x{{'?', 1}, {'|', 2}, {INTCONST, 5}, {-1, 6}}},
			{"???/\n2", x{{'?', 1}, {INTCONST, 6}, {-1, 7}}},
			{"????!0", x{{'?', 1}, {'?', 2}, {'|', 3}, {INTCONST, 6}, {-1, 7}}},
			{"???x0", x{{'?', 1}, {'?', 2}, {'?', 3}, {IDENTIFIER, 4}, {-1, 6}}},
			{"???x??!0", x{{'?', 1}, {'?', 2}, {'?', 3}, {IDENTIFIER, 4}, {'|', 5}, {INTCONST, 8}, {-1, 9}}},
			{"??x0", x{{'?', 1}, {'?', 2}, {IDENTIFIER, 3}, {-1, 5}}},
			{"??x??!0", x{{'?', 1}, {'?', 2}, {IDENTIFIER, 3}, {'|', 4}, {INTCONST, 7}, {-1, 8}}},
			{"?x0", x{{'?', 1}, {IDENTIFIER, 2}, {-1, 4}}},
			{"?x??!0", x{{'?', 1}, {IDENTIFIER, 2}, {'|', 3}, {INTCONST, 6}, {-1, 7}}},
			{"@", x{{'@', 1}, {-1, 2}}},
			{"@%", x{{'@', 1}, {'%', 2}, {-1, 3}}},
			{"@%0", x{{'@', 1}, {'%', 2}, {INTCONST, 3}, {-1, 4}}},
			{"@%:", x{{'@', 1}, {'#', 2}, {-1, 4}}},
			{"@%:0", x{{'@', 1}, {'#', 2}, {INTCONST, 4}, {-1, 5}}},
			{"@%:01", x{{'@', 1}, {'#', 2}, {INTCONST, 4}, {-1, 6}}},
			{"@??=", x{{'@', 1}, {'#', 2}, {-1, 5}}},
			{"\"(a\\\nz", x{{'"', 1}, {'(', 2}, {IDENTIFIER, 3}, {-1, 7}}},
			{"\\1\n", x{{'\\', 1}, {INTCONST, 2}, {'\n', 3}, {-1, 4}}},
			{"\\1\n2", x{{'\\', 1}, {INTCONST, 2}, {'\n', 3}, {INTCONST, 4}, {-1, 5}}},
			{"\\\n", x{{-1, 3}}},
			{"\\\n2", x{{INTCONST, 3}, {-1, 4}}},
			{"\\\r\n", x{{-1, 4}}},
			{"\\\r\n2", x{{INTCONST, 4}, {-1, 5}}},
			{"\r", x{{-1, 2}}},
			{"\r0", x{{INTCONST, 2}, {-1, 3}}},
			{"\r01", x{{INTCONST, 2}, {-1, 4}}},
			{"\x00", x{{0, 1}, {-1, 2}}},
			{"\x000", x{{0, 1}, {INTCONST, 2}, {-1, 3}}},
		},
	)
}

func testPreprocessorExample(t *testing.T, fname string) string {
	var buf bytes.Buffer
	_, err := Parse(
		"",
		[]string{fname},
		newTestModel(),
		preprocessOnly(),
		Cpp(func(toks []xc.Token) {
			//dbg("____ cpp toks\n%s", PrettyString(toks))
			for _, v := range toks {
				buf.WriteString(TokSrc(v))
			}
			buf.WriteByte('\n')
		}),
	)
	if err != nil {
		t.Fatal(errString(err))
	}
	return strings.TrimSpace(buf.String())
}

func TestStdExample6_10_3_3_4(t *testing.T) {
	if g, e := testPreprocessorExample(t, "testdata/example-6.10.3.3-4.h"), `char p[] = "x ## y";`; g != e {
		t.Fatalf("\ngot\n%s\nexp\n%s", g, e)
	}
}

func TestStdExample6_10_3_5_3(t *testing.T) {
	if g, e := testPreprocessorExample(t, "testdata/example-6.10.3.5-3.h"),
		`f(2 * (y+1)) + f(2 * (f(2 * (z[0])))) % f(2 * (0)) + t(1);
f(2 * (2+(3,4)-0,1)) | f(2 * (~ 5)) &
f(2 * (0,1))^m(0,1);
int i[] = { 1, 23, 4, 5,  };
char c[2][6] = { "hello", "" };`; g != e {
		t.Fatalf("\ngot\n%s\nexp\n%s", g, e)
	}
}

func TestStdExample6_10_3_5_4(t *testing.T) {
	if g, e := testPreprocessorExample(t, "testdata/example-6.10.3.5-4.h"),
		`printf("x1= %d, x2= %s", x1, x2);
fputs(
"strncmp(\"abc\\0d\", \"abc\", '\\4') == 0: @\n", s);
vers2.h included from testdata/example-6.10.3.5-4.h
"hello";
"hello, world"`; g != e {
		t.Fatalf("\ngot\n%s\nexp\n%s", g, e)
	}
}

func TestStdExample6_10_3_5_5(t *testing.T) {
	if g, e := testPreprocessorExample(t, "testdata/example-6.10.3.5-5.h"),
		`int j[] = { 123, 45, 67, 89,
10, 11, 12,  };`; g != e {
		t.Fatalf("\ngot\n%s\nexp\n%s", g, e)
	}
}

func TestStdExample6_10_3_5_6(t *testing.T) {
	if g, e := testPreprocessorExample(t, "testdata/example-6.10.3.5-6.h"),
		`ok`; g != e {
		t.Fatalf("\ngot\n%s\nexp\n%s", g, e)
	}
}

func TestStdExample6_10_3_5_7(t *testing.T) {
	if g, e := testPreprocessorExample(t, "testdata/example-6.10.3.5-7.h"),
		`fprintf(stderr, "Flag");
fprintf(stderr, "X = %d\n", x);
puts("The first, second, and third items.");
((x>y)?puts("x>y"): printf("x is %d but y is %d", x, y));`; g != e {
		t.Fatalf("\ngot\n%s\nexp\n%s", g, e)
	}
}

func testDev(t *testing.T, predefine string, cppOpts, src []string, opts ...Opt) {
	for _, v := range src {
		fi, err := os.Stat(v)
		if err != nil {
			if os.IsNotExist(err) {
				t.Logf("skipping: %v", err)
				return
			}
		}

		if !fi.Mode().IsRegular() {
			t.Errorf("%s: not a regular file", v)
			return
		}
	}

	t.Log(src)

	logf, err := os.Create("log-" + filepath.Base(src[len(src)-1]))
	if err != nil {
		t.Fatal(err)
	}

	defer logf.Close()

	logw := bufio.NewWriter(logf)

	defer logw.Flush()

	var got, exp []xc.Token

	_, err = Parse(
		predefine,
		src,
		newTestModel(),
		append(
			opts,
			preprocessOnly(),
			Cpp(func(toks []xc.Token) {
				for _, v := range toks {
					logw.WriteString(TokSrc(toC(v)))
					if v.Rune != ' ' {
						got = append(got, v)
					}
				}
				logw.WriteByte('\n')
			}),
			disableWarnings(),
		)...,
	)
	if err != nil {
		t.Error(errString(err))
		return
	}

	out, err := exec.Command("cpp", append(cppOpts, src...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v", src, err)
		return
	}

	f, err := ioutil.TempFile("", "cc-test-")
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if !*oTmp {
			os.Remove(f.Name())
		}
		f.Close()
	}()

	if _, err := f.Write(out); err != nil {
		t.Fatal(err)
	}

	if _, err := Parse(
		predefine,
		[]string{f.Name()},
		newTestModel(),
		append(
			opts,
			preprocessOnly(),
			Cpp(func(toks []xc.Token) {
				for _, tok := range toks {
					if tok.Rune != ' ' {
						exp = append(exp, tok)
					}
				}
			}),
			disableWarnings(),
		)...,
	); err != nil {
		t.Error(err)
		return
	}

	if g, e := len(got), len(exp); g != e {
		t.Errorf("%v: got %d tokens, expected %d tokens (∆ %d)", src, g, e, g-e)
		switch {
		case g < e:
			exp = exp[:g]
		default:
			got = got[:e]
		}
	}

	for i, g := range got {
		g = toC(g)
		e := toC(exp[i])
		if g.Rune != e.Rune || g.Val != e.Val &&

			// Whitelist
			!strings.HasPrefix(string(xc.Dict.S(g.Val)), "\"VIM - Vi IMproved 7.4 (2013 Aug 10,") {

			t.Errorf("%d\ngot %s\nexp %s", i, PrettyString(g), PrettyString(e))
			return
		}
	}

	if src[0] == "testdata/arith-1.c" {
		return
	}

	_, err = Parse(
		predefine,
		src,
		newTestModel(),
		append(
			opts,
			disableWarnings(),
		)...,
	)
	if err != nil {
		t.Error(errString(err))
		return
	}
}

func dirExists(t *testing.T, dir string) bool {
	dir = filepath.FromSlash(dir)
	fi, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}

		t.Fatal(err)
	}

	if !fi.IsDir() {
		t.Fatal(err)
	}

	return true
}

func TestDev(t *testing.T) {
	predefined, err := cppPredefined()
	if err != nil {
		t.Logf("skipping: %v", err)
		return
	}

	predefined += fmt.Sprintf(`
#define __DATE__ %q
#define __TIME__ %q

#define __PRETTY_FUNCTION__ __func__
#define __asm__(x)
#define __attribute__(...)
#define __builtin_bswap32(bsx) ( 0U )
#define __builtin_bswap64(bsx) ( (__uint64_t)0 )
#define __builtin_offsetof(st, m) ((size_t)(&((st *)0)->m))
#define __builtin_va_arg(ap, type) ( *( type* )ap )
#define __builtin_va_end(v)
#define __builtin_va_list void*
#define __builtin_va_start(x, y)
#define __extension__
#define __inline
#define __restrict
#define __volatile__

double __builtin_nanf(char *);
double __builtin_inff();
`, xc.Dict.S(idTDate), xc.Dict.S(idTTtime))

	sysIncludePaths, err := cppSysIncludePaths()
	if err != nil {
		t.Logf("skipping: %v", err)
		return
	}

	sysIncludes := SysIncludePaths(sysIncludePaths)

	testDev(t, predefined, nil, []string{"testdata/arith-1.c"})

	if !dirExists(t, "testdata/dev/sqlite3") {
		return
	}

	opts := []Opt{
		sysIncludes,
		EnableAnonymousStructFields(),
	}

	testDev(t, predefined, nil, []string{"testdata/dev/sqlite3/shell.c"}, opts...)
	testDev(t, predefined, nil, []string{"testdata/dev/sqlite3/sqlite3.c"}, opts...)
	testDev(t, predefined, nil, []string{"testdata/dev/sqlite3/sqlite3.h"}, opts...)
	testDev(t, predefined, nil, []string{"testdata/dev/sqlite3/sqlite3ext.h"}, opts...)

	if !dirExists(t, "testdata/dev/vim") {
		return
	}

	predefine := predefined + `
#define HAVE_CONFIG_H
#define _FORTIFY_SOURCE 1
`
	opts = []Opt{
		IncludePaths([]string{
			"testdata/dev/vim/vim/src/",
			"testdata/dev/vim/vim/src/proto/",
		}),
		sysIncludes,
		EnableIncludeNext(),
		EnableAnonymousStructFields(),
	}
	if *oFailFast {
		opts = append(opts, CrashOnError())
	}

	cppOpts := []string{
		"-Itestdata/dev/vim/vim/src/",
		"-Itestdata/dev/vim/vim/src/proto",
		"-DHAVE_CONFIG_H",
		"-U_FORTIFY_SOURCE",
		"-D_FORTIFY_SOURCE=1",
	}
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/buffer.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/blowfish.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/charset.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/crypt.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/crypt_zip.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/diff.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/digraph.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/edit.c"}, opts...)
	//TODO testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/eval.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/ex_cmds.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/ex_cmds2.c"}, opts...)
	//TODO testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/ex_docmd.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/ex_eval.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/ex_getln.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/fileio.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/fold.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/getchar.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/hardcopy.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/hashtab.c"}, opts...)
	//TODO testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/if_cscope.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/if_xcmdsrv.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/mark.c"}, opts...)
	//TODO testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/memline.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/menu.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/message.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/misc1.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/misc2.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/move.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/mbyte.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/normal.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/ops.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/option.c"}, opts...)
	//TODO testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/os_unix.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/auto/pathdef.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/popupmnu.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/quickfix.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/regexp.c"}, opts...)
	//TODO testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/screen.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/search.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/sha256.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/spell.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/syntax.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/tag.c"}, opts...)
	//TODO testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/term.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/ui.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/undo.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/window.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/netbeans.c"}, opts...)
	//TODO testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/channel.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/json.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/main.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/memfile.c"}, opts...)
	testDev(t, predefine, cppOpts, []string{"testdata/dev/vim/vim/src/version.c"}, opts...)
}

func TestPPParse1(t *testing.T) {
	path := *o1
	if path == "" {
		return
	}

	testReport := newTestReport()
	testReport.ClearErrors()
	_, err := ppParse(path, testReport, testTweaks)
	if err != nil {
		t.Fatal(err)
	}

	if err := testReport.Errors(true); err != nil {
		t.Fatal(errString(err))
	}
}

func testPreprocess1(t *testing.T, paths []string) {
	testReport := xc.NewReport()
	testReport.ClearErrors()
	predefined, err := ppParseString("", predefinedMacros, testReport, testTweaks)
	if err != nil {
		t.Fatal(err)
	}

	model := newTestModel()
	macros := newMacros()
	func() {
		ch := make(chan []xc.Token, 1000)
		defer close(ch)

		go func() {
			for range ch {
			}
		}()

		newPP(ch, includes, sysIncludes, macros, false, model, testReport, testTweaks).preprocessingFile(predefined)
		if err := testReport.Errors(true); err != nil {
			t.Fatal(errString(err))
		}

		for _, path := range paths {
			ast, err := ppParse(path, testReport, testTweaks)
			if err != nil {
				t.Fatal(err)
			}

			newPP(ch, includes, sysIncludes, macros, true, model, testReport, testTweaks).preprocessingFile(ast)
			if err := testReport.Errors(true); err != nil {
				t.Fatal(errString(err))
			}
		}
	}()
}

func TestPreprocess(t *testing.T) {
	const arith = "testdata/arith-1.c"
	a := []string{}
	if _, err := os.Stat(arith); err == nil {
		a = []string{arith}
	}

	for _, v := range a {
		testPreprocess1(t, []string{v})
	}
}

func TestFinalInjection(t *testing.T) {
	const src = "int f() {}"

	if strings.HasSuffix(src, "\n") {
		t.Fatal("internal error")
	}

	ast, err := ppParseString("test.c", src, xc.NewReport(), &tweaks{})
	if err != nil {
		t.Fatal(errString(err))
	}

	ast2, err := ppParseString("test.c", src+"\n", xc.NewReport(), &tweaks{})
	if err != nil {
		t.Fatal(errString(err))
	}

	if g, e := PrettyString(ast2), PrettyString(ast); g != e {
		t.Fatalf("got\n%s\nexpected\n%s", g, e)
	}

	t.Log(PrettyString(ast))
}

func TestRedecl(t *testing.T) {
	testParse(t, []string{"testdata/redecl.c"}, "")
}

func testParse(t *testing.T, paths []string, ignoreError string, opts ...Opt) *TranslationUnit {
	last := paths[len(paths)-1]
	ln := filepath.Base(last)
	f, err := os.Create("log-" + ln)
	if err != nil {
		t.Fatal(err)
	}

	defer f.Close()

	b := bufio.NewWriter(f)
	defer b.Flush()

	b.WriteString("// +build ignore\n\n")
	var a []string
	crash := nopOpt()
	if *oFailFast {
		crash = CrashOnError()
	}
	opts = append(
		opts,
		IncludePaths(includes),
		SysIncludePaths(sysIncludes),
		Cpp(func(toks []xc.Token) {
			a = a[:0]
			for _, v := range toks {
				a = append(a, TokSrc(v))
			}
			fmt.Fprintf(b, "%s\n", strings.Join(a, " "))
		}),
		crash,
		ErrLimit(-1),
	)
	ast, err := Parse(
		predefinedMacros,
		paths,
		newTestModel(),
		opts...,
	)
	if err != nil {
		if s := strings.TrimSpace(errString(err)); s != ignoreError {
			t.Fatal(errString(err))
		}
	}

	t.Log(paths)
	return ast
}

func ddsStr(dds []*DirectDeclarator) string {
	buf := bytes.NewBufferString("|")
	for i, dd := range dds {
		switch dd.Case {
		case 0: // IDENTIFIER
			buf.WriteString("IDENTIFIER")
		case 1: // '(' Declarator ')'
			buf.WriteString("(")
			buf.WriteString(strings.Repeat("*", dd.Declarator.stars()))
			fmt.Fprintf(buf, "Declarator.%v)", dds[i-1].Case)
		case 2: // DirectDeclarator '[' TypeQualifierListOpt ExpressionOpt ']'
			fmt.Fprintf(buf, "DirectDeclarator[TypeQualifierListOpt ExpressionOpt.%v]", dd.elements)
		case 3: // DirectDeclarator '[' "static" TypeQualifierListOpt Expression ']'
			fmt.Fprintf(buf, "DirectDeclarator[static TypeQualifierListOpt Expression.%v]", dd.elements)
		case 4: // DirectDeclarator '[' TypeQualifierList "static" Expression ']'
			fmt.Fprintf(buf, "DirectDeclarator[TypeQualifierList static Expression.%v]", dd.elements)
		case 5: // DirectDeclarator '[' TypeQualifierListOpt '*' ']'
			fmt.Fprintf(buf, "DirectDeclarator[TypeQualifierListOpt*.%v]", dd.elements)
		case 6: // DirectDeclarator '(' ParameterTypeList ')'
			buf.WriteString("DirectDeclarator(ParameterTypeList)")
		case 7: // DirectDeclarator '(' IdentifierListOpt ')'
			buf.WriteString("DirectDeclarator(IdentifierListOpt)")
		}
		buf.WriteString("|")
	}
	return buf.String()
}

func (n *ctype) str() string {
	return fmt.Sprintf("R%v S%v %v", n.resultStars, n.stars, ddsStr(n.dds))
}

func TestIssue3(t *testing.T) {
	if _, err := Parse("", []string{"testdata/issue3.h"}, newTestModel()); err != nil {
		t.Fatal(errString(err))
	}
}

func TestIssue8(t *testing.T) {
	if _, err := Parse("", []string{"testdata/issue8.h"}, newTestModel()); err != nil {
		t.Fatal(errString(err))
	}
}

func TestIssue4(t *testing.T) {
	_, err := Parse("", []string{"testdata/issue4.c"}, newTestModel())
	if err == nil {
		t.Fatal("unexpected sucess")
	}

	l, ok := err.(scanner.ErrorList)
	if !ok {
		t.Fatalf("unexpected error type %T", err)
	}

	if g, e := l.Len(), 2; g != e {
		t.Fatal(g, e)
	}

	if g, e := l[0].Error(), "testdata/issue4.c:5:13: redeclaration of foo as different kind of symbol, previous declaration at testdata/issue4.c:4:5"; g != e {
		t.Fatal(g, e)
	}

	if g, e := l[1].Error(), "testdata/issue4.c:9:15: redeclaration of foo2 as different kind of symbol, previous declaration at testdata/issue4.c:8:7"; g != e {
		t.Fatal(g, e)
	}
}

func unpackType(typ Type) Type {
	for {
		switch typ.Kind() {
		case Ptr, Array:
			typ = typ.Element()
		default:
			return typ
		}
	}
}

func TestIssue9(t *testing.T) {
	const exp = `original:  typedef short[64] Array
unpacked:  typedef short Short
original: JBLOCK short(*)[64] Ptr
unpacked: JBLOCK short Short
original: JBLOCKROW short(**)[64] Ptr
unpacked: JBLOCKROW short Short
original: JBLOCKARRAY short(***)[64] Ptr
unpacked: JBLOCKARRAY short Short
original:  short[64] Array
unpacked:  short Short
original:  short[64] Array
unpacked:  short Short
original: JBLOCK short[64] Array
unpacked: JBLOCK short Short
original: JBLOCK short[64] Array
unpacked: JBLOCK short Short
original:  short(*)[64] Ptr
unpacked:  short Short
original:  short(*)[64] Ptr
unpacked:  short Short
original:  short(*)[64] Ptr
unpacked:  short Short
original: JBLOCKROW short(*)[64] Ptr
unpacked: JBLOCKROW short Short
original: JBLOCKROW short(*)[64] Ptr
unpacked: JBLOCKROW short Short
original:  short(**)[64] Ptr
unpacked:  short Short
original:  short(**)[64] Ptr
unpacked:  short Short
original:  short(**)[64] Ptr
unpacked:  short Short
original: JBLOCKARRAY short(**)[64] Ptr
unpacked: JBLOCKARRAY short Short
original: JBLOCKARRAY short(**)[64] Ptr
unpacked: JBLOCKARRAY short Short
original:  short(***)[64] Ptr
unpacked:  short Short
original:  short(***)[64] Ptr
unpacked:  short Short
original:  short(***)[64] Ptr
unpacked:  short Short
original: JBLOCKIMAGE short(***)[64] Ptr
unpacked: JBLOCKIMAGE short Short
original: JBLOCKIMAGE short(***)[64] Ptr
unpacked: JBLOCKIMAGE short Short
`

	tu, err := Parse("", []string{"testdata/issue9.c"}, newTestModel())
	if err != nil {
		t.Fatal(errString(err))
	}

	var buf bytes.Buffer
	for tu != nil {
		declr := tu.ExternalDeclaration.Declaration.InitDeclaratorListOpt.InitDeclaratorList.InitDeclarator.Declarator
		name := string(xc.Dict.S(declr.RawSpecifier().TypedefName()))
		fmt.Fprintln(&buf, "original:", name, declr.Type, declr.Type.Kind())
		unpacked := unpackType(declr.Type)
		fmt.Fprintln(&buf, "unpacked:", name, unpacked, unpacked.Kind())
		tu = tu.TranslationUnit
	}
	if g, e := buf.String(), exp; g != e {
		t.Fatalf("got:\n%s\nexp:\n%s", g, e)
	}
}

func TestEnumConstToks(t *testing.T) {
	tu, err := Parse("", []string{"testdata/enum.c"}, newTestModel())
	if err != nil {
		t.Fatal(errString(err))
	}

	sc := tu.Declarations
	foo := sc.Lookup(NSIdentifiers, xc.Dict.SID("foo"))
	if foo.Node == nil {
		t.Fatal("internal error")
	}

	switch x := foo.Node.(type) {
	case *DirectDeclarator:
		typ := x.TopDeclarator().Type
		if g, e := typ.Kind(), Enum; g != e {
			t.Fatal(g, e)
		}

		l := typ.EnumeratorList()
		if g, e := PrettyString(
			l),
			`[]cc.EnumConstant{ // len 2
· 0: cc.EnumConstant{
· · DefTok: testdata/enum.c:4:2: IDENTIFIER "c",
· · Value: 18,
· · Tokens: []xc.Token{ // len 3
· · · 0: testdata/enum.c:4:6: INTCONST "42",
· · · 1: testdata/enum.c:4:9: '-',
· · · 2: testdata/enum.c:4:11: INTCONST "24",
· · },
· },
· 1: cc.EnumConstant{
· · DefTok: testdata/enum.c:5:2: IDENTIFIER "d",
· · Value: 592,
· · Tokens: []xc.Token{ // len 3
· · · 0: testdata/enum.c:5:6: INTCONST "314",
· · · 1: testdata/enum.c:5:10: '+',
· · · 2: testdata/enum.c:5:12: INTCONST "278",
· · },
· },
}`; g != e {
			t.Fatalf("got\n%s\nexp\n%s", g, e)
		}
		var a []string
		for _, e := range l {
			var b []string
			for _, t := range e.Tokens {
				b = append(b, TokSrc(t))
			}
			a = append(a, strings.Join(b, " "))
		}
		if g, e := strings.Join(a, "\n"), "42 - 24\n314 + 278"; g != e {
			t.Fatalf("got\n%s\nexp\n%s", g, e)
		}
	default:
		t.Fatalf("%T", x)
	}
}

func TestPaste(t *testing.T) {
	testParse(t, []string{"testdata/paste.c"}, "")
}

func TestPaste2(t *testing.T) {
	testParse(t, []string{"testdata/paste2.c"}, "")
}

func TestFunc(t *testing.T) {
	testParse(t, []string{"testdata/func.c"}, "")
}

func TestEmptyMacroArg(t *testing.T) {
	testParse(t, []string{"testdata/empty.c"}, "")
}

func TestFuncFuncParams(t *testing.T) {
	testParse(t, []string{"testdata/funcfunc.c"}, "")
}

func TestAnonStructField(t *testing.T) {
	//testParse(t, []string{"testdata/anon.c"})
	testParse(
		t,
		[]string{"testdata/anon.c"},
		"testdata/anon.c:4:7: only unnamed structs and unions are allowed",
		EnableAnonymousStructFields(),
	)
}
