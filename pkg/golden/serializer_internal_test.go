package golden

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// fixtureWithDecoded is a minimal valid fixture whose decoded section is the
// given JSON, for exercising the flattening and encoding seams directly.
func fixtureWithDecoded(decoded string) *Fixture {
	return &Fixture{
		Format:  FormatVersion,
		Type:    "TransactionEnvelope",
		XDR:     "AAAA",
		Decoded: json.RawMessage(decoded),
	}
}

func TestScalarText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "nil", in: nil, want: "null"},
		{name: "true", in: true, want: "true"},
		{name: "false", in: false, want: "false"},
		{name: "plain string", in: "testnet", want: "testnet"},
		{name: "strkey", in: "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6",
			want: "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"},
		{name: "number-like string", in: "58002", want: `"58002"`},
		{name: "empty string", in: "", want: `""`},
		{name: "padded string", in: " x ", want: `" x "`},
		{name: "reserved word", in: "yes", want: `"yes"`},
		{name: "colon", in: "a: b", want: `"a: b"`},
		{name: "comment marker", in: "a # b", want: `"a # b"`},
		{name: "leading dash", in: "- dash", want: `"- dash"`},
		{name: "control character", in: "a\nb", want: `"a\nb"`},
		{name: "number", in: json.Number("58002"), want: "58002"},
		{name: "negative number", in: json.Number("-1"), want: "-1"},
		{name: "float", in: json.Number("1.5"), want: "1.5"},
		// A json.Number that is not a number cannot be written bare without
		// being read back as something else.
		{name: "bogus number", in: json.Number("nope"), want: `"nope"`},
		// Values arrive from JSON, so the fallback is only for a value built
		// in Go; it still must not produce an empty line.
		{name: "unsupported type", in: 7, want: `"7"`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := scalarText(tc.in); got != tc.want {
				t.Errorf("scalarText(%#v) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestPlainSafe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "word", in: "testnet", want: true},
		{name: "path", in: "focus/v1", want: true},
		{name: "base64", in: "AAAAAgAAAAD/Uy8=", want: true},
		{name: "dotted key", in: "key.with.dot", want: true},
		{name: "empty", in: "", want: false},
		{name: "leading space", in: " x", want: false},
		{name: "trailing space", in: "x ", want: false},
		{name: "reserved yes", in: "YES", want: false},
		{name: "reserved tilde", in: "~", want: false},
		{name: "reserved inf", in: ".inf", want: false},
		{name: "leading dash", in: "-x", want: false},
		{name: "leading question", in: "?x", want: false},
		{name: "leading colon", in: ":x", want: false},
		{name: "leading comma", in: ",x", want: false},
		{name: "leading bracket", in: "[x]", want: false},
		{name: "leading brace", in: "{x}", want: false},
		{name: "leading hash", in: "#x", want: false},
		{name: "leading amp", in: "&x", want: false},
		{name: "leading star", in: "*x", want: false},
		{name: "leading bang", in: "!x", want: false},
		{name: "leading pipe", in: "|x", want: false},
		{name: "leading gt", in: ">x", want: false},
		{name: "leading quote", in: "'x", want: false},
		{name: "leading double quote", in: `"x`, want: false},
		{name: "leading percent", in: "%x", want: false},
		{name: "leading at", in: "@x", want: false},
		{name: "leading backtick", in: "`x", want: false},
		{name: "leading backslash", in: `\x`, want: false},
		{name: "inner colon", in: "a:b", want: false},
		{name: "inner hash", in: "a#b", want: false},
		{name: "control character", in: "a\x00b", want: false},
		{name: "delete character", in: "a\x7fb", want: false},
		{name: "number", in: "123", want: false},
		{name: "leading zero", in: "00", want: false},
		{name: "hex", in: "0x1f", want: false},
		{name: "float", in: "1.5", want: false},
		{name: "exponent", in: "1e5", want: false},
		{name: "secret exponent form", in: "infinity", want: false},
		{name: "json true", in: "true", want: false},
		{name: "json document", in: `{"a":1}`, want: false},
		{name: "json array", in: "[1,2]", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := plainSafe(tc.in); got != tc.want {
				t.Errorf("plainSafe(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want bool
	}{
		{in: "0", want: true},
		{in: "-1", want: true},
		{in: "1.5", want: true},
		{in: "1e5", want: true},
		{in: "0x1f", want: true},
		{in: "1_000", want: true},
		{in: "NaN", want: true},
		{in: "Inf", want: true},
		{in: "testnet", want: false},
		{in: "1.5.5", want: false},
		{in: "", want: false},
	}

	for _, tc := range tests {
		if got := isNumber(tc.in); got != tc.want {
			t.Errorf("isNumber(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestQuotedScalars(t *testing.T) {
	t.Parallel()

	singles := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "plain", in: "'abc'", want: "abc"},
		{name: "empty", in: "''", want: ""},
		{name: "doubled quote", in: "'it''s'", want: "it's"},
		{name: "no closing quote", in: "'abc", wantErr: true},
		{name: "lone quote", in: "'", wantErr: true},
		{name: "stray quote", in: "'it's'", wantErr: true},
	}
	for _, tc := range singles {
		t.Run("single/"+tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseSingleQuoted(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseSingleQuoted(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSingleQuoted(%q) error = %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseSingleQuoted(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	quoted := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "double", in: `"abc"`, want: "abc"},
		{name: "escaped quote", in: `"a\"b"`, want: `a"b`},
		{name: "single", in: "'abc'", want: "abc"},
		{name: "malformed double", in: `"abc`, wantErr: true},
		{name: "malformed single", in: "'abc", wantErr: true},
	}
	for _, tc := range quoted {
		t.Run("unquote/"+tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := unquoteYAML(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("unquoteYAML(%q) = %q, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unquoteYAML(%q) error = %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("unquoteYAML(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	scans := []struct {
		name    string
		in      string
		quote   byte
		want    int
		wantErr bool
	}{
		{name: "double", in: `"ab" rest`, quote: '"', want: 4},
		{name: "escaped quote inside", in: `"a\"b"`, quote: '"', want: 6},
		{name: "single", in: "'ab' rest", quote: '\'', want: 4},
		{name: "doubled quote inside", in: "'it''s' rest", quote: '\'', want: 7},
		{name: "unterminated double", in: `"ab`, quote: '"', wantErr: true},
		{name: "unterminated single", in: "'ab", quote: '\'', wantErr: true},
	}
	for _, tc := range scans {
		t.Run("scan/"+tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := scanQuoted(tc.in, tc.quote)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("scanQuoted(%q) = %d, want an error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("scanQuoted(%q) error = %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("scanQuoted(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseScalarToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    any
		wantErr string
	}{
		{name: "empty", in: "", want: nil},
		{name: "spaces", in: "   ", want: nil},
		{name: "single quoted", in: "'it''s'", want: "it's"},
		{name: "double quoted", in: `"a b"`, want: "a b"},
		{name: "number", in: "58002", want: json.Number("58002")},
		{name: "boolean", in: "true", want: true},
		{name: "json null", in: "null", want: nil},
		{name: "yaml null", in: "~", want: nil},
		{name: "YAML NULL case", in: "NULL", want: nil},
		{name: "FALSE case", in: "FALSE", want: false},
		{name: "bare string", in: "testnet", want: "testnet"},
		{name: "padded bare string", in: "  testnet  ", want: "testnet"},
		{name: "malformed double", in: `"abc`, wantErr: "malformed double-quoted"},
		{name: "malformed single", in: "'abc", wantErr: "malformed single-quoted"},
		{name: "control character", in: "a\x00b", wantErr: "control character"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseScalarToken(tc.in)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parseScalarToken(%q) = %#v, want an error", tc.in, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseScalarToken(%q) error = %v", tc.in, err)
			}
			if !equalScalar(got, tc.want) {
				t.Errorf("parseScalarToken(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// equalScalar compares two scalar tokens; the types must match, since a string
// read back as a number is exactly the bug quoting exists to prevent.
func equalScalar(got, want any) bool {
	if g, ok := got.(json.Number); ok {
		w, ok := want.(json.Number)
		return ok && g == w
	}
	return got == want
}

func TestSplitYAMLKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       string
		wantKey  string
		wantRest string
		wantOK   bool
	}{
		{name: "pair", in: "key: value", wantKey: "key", wantRest: "value", wantOK: true},
		{name: "empty value", in: "key:", wantKey: "key", wantRest: "", wantOK: true},
		{name: "empty value with space", in: "key:   ", wantKey: "key", wantRest: "", wantOK: true},
		{name: "colon in value", in: "at: 2026-08-05T12:00:00Z", wantKey: "at", wantRest: "2026-08-05T12:00:00Z", wantOK: true},
		{name: "quoted key", in: `"colon: key": value`, wantKey: "colon: key", wantRest: "value", wantOK: true},
		{name: "single quoted key", in: `'a key': value`, wantKey: "a key", wantRest: "value", wantOK: true},
		{name: "quoted key with no separator", in: `"key" value`, wantOK: false},
		{name: "no colon", in: "just words", wantOK: false},
		{name: "colon without space", in: "https://example.com", wantOK: false},
		{name: "empty", in: "", wantOK: false},
		{name: "unterminated quote", in: `"key: value`, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			key, rest, ok := splitYAMLKey(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("splitYAMLKey(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if key != tc.wantKey || rest != tc.wantRest {
				t.Errorf("splitYAMLKey(%q) = (%q, %q), want (%q, %q)",
					tc.in, key, rest, tc.wantKey, tc.wantRest)
			}
		})
	}
}

func TestSplitYAMLLines(t *testing.T) {
	t.Parallel()

	t.Run("drops noise and expands the compact form", func(t *testing.T) {
		t.Parallel()

		const doc = "---\n" +
			"# a comment\n" +
			"\n" +
			"format: focus/v1\n" +
			"list:\n" +
			"  - name: a\n" +
			"    ok: true\n" +
			"  - - 1\n" +
			"  - plain\n" +
			"...\n"

		lines, err := splitYAMLLines([]byte(doc))
		if err != nil {
			t.Fatalf("splitYAMLLines() error = %v", err)
		}

		var got []string
		for _, l := range lines {
			got = append(got, strings.Repeat(" ", l.indent)+l.text)
		}
		want := []string{
			"format: focus/v1",
			"list:",
			"  -",
			"    name: a",
			"    ok: true",
			"  -",
			"    - 1",
			"  - plain",
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("splitYAMLLines() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	})

	t.Run("rejects a tab indent", func(t *testing.T) {
		t.Parallel()

		if _, err := splitYAMLLines([]byte("format: focus/v1\n\txdr: AAAA\n")); err == nil {
			t.Error("a tab indentation was accepted")
		}
	})
}

func TestCompactSeqBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       string
		wantRest string
		wantOK   bool
	}{
		{name: "compact mapping", in: "- name: a", wantRest: "name: a", wantOK: true},
		{name: "compact sequence", in: "- - 1", wantRest: "- 1", wantOK: true},
		{name: "bare dash", in: "-", wantOK: false},
		{name: "plain scalar", in: "- plain", wantOK: false},
		{name: "negative number", in: "- -1", wantOK: false},
		{name: "not a sequence entry", in: "name: a", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rest, ok := compactSeqBody(tc.in)
			if ok != tc.wantOK || rest != tc.wantRest {
				t.Errorf("compactSeqBody(%q) = (%q, %v), want (%q, %v)",
					tc.in, rest, ok, tc.wantRest, tc.wantOK)
			}
		})
	}
}

func TestYAMLUnmarshalRejectsMalformedDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		doc     string
		wantErr string
	}{
		{
			name:    "indentation jump",
			doc:     "format: focus/v1\nxdr: AAAA\ncaptured:\n    network: t\n  ledger: 1\n",
			wantErr: "unexpected indentation",
		},
		{
			name:    "sequence entry in a mapping",
			doc:     "format: focus/v1\n- x\n",
			wantErr: "sequence entry cannot appear inside a mapping",
		},
		{
			name:    "explicit key",
			doc:     "? format\n: focus/v1\n",
			wantErr: "explicit keys",
		},
		{
			name:    "line that is not a pair",
			doc:     "format: focus/v1\njust words\n",
			wantErr: `expected "key: value"`,
		},
		{
			name:    "duplicate key",
			doc:     "format: focus/v1\nformat: focus/v1\n",
			wantErr: "duplicate key",
		},
		{
			name:    "nested block on the same line",
			doc:     "format: focus/v1\nxdr: - a\n",
			wantErr: "nested block must be indented",
		},
		{
			name:    "malformed double-quoted value",
			doc:     "format: focus/v1\nxdr: \"AAAA\n",
			wantErr: "malformed double-quoted",
		},
		{
			name:    "control character in a value",
			doc:     "format: focus/v1\nxdr: a\x00b\n",
			wantErr: "control character",
		},
		{
			name:    "sequence entry expected",
			doc:     "format: focus/v1\nxdr: AAAA\nlist:\n  - a\n  b: 1\n",
			wantErr: `expected a "-" sequence entry`,
		},
		{
			name:    "trailing content after an indented document",
			doc:     "  format: focus/v1\n  xdr: AAAA\nextra: 1\n",
			wantErr: "unexpected trailing content",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			serializer, err := SerializerFor(YAML)
			if err != nil {
				t.Fatalf("SerializerFor() error = %v", err)
			}
			_, err = serializer.Unmarshal([]byte(tc.doc))
			if err == nil {
				t.Fatalf("Unmarshal() accepted a malformed document:\n%s", tc.doc)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
			}
			// A parse error must say where it happened.
			if !strings.Contains(err.Error(), "line ") {
				t.Errorf("error does not name the line: %v", err)
			}
		})
	}
}

// TestYAMLWritesEmptyContainers covers the two inline forms the writer uses
// inside a sequence, which are the only flow style in the subset.
func TestYAMLWritesEmptyContainers(t *testing.T) {
	t.Parallel()

	serializer, err := SerializerFor(YAML)
	if err != nil {
		t.Fatalf("SerializerFor() error = %v", err)
	}
	data, err := serializer.Marshal(fixtureWithDecoded(`{"nested":[[],{}]}`))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "nested:\n    - []\n    - {}\n") {
		t.Errorf("empty containers are not written as flow forms:\n%s", content)
	}

	got, err := serializer.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if string(got.Decoded) != `{"nested":[[],{}]}` {
		t.Errorf("empty containers did not round-trip: %s", got.Decoded)
	}
}

func TestYAMLKeyOrderIsNotMapOrder(t *testing.T) {
	t.Parallel()

	// Sorted keys and a fixed field order are what make the output stable
	// across runs; a Go map would reorder these.
	var want []string
	for _, key := range []string{"alpha", "beta", "gamma", "zulu"} {
		want = append(want, key+": ")
	}

	serializer, err := SerializerFor(YAML)
	if err != nil {
		t.Fatalf("SerializerFor() error = %v", err)
	}
	data, err := serializer.Marshal(fixtureWithDecoded(
		`{"zulu":4,"alpha":1,"gamma":3,"beta":2}`))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	content := string(data)
	last := -1
	for _, key := range want {
		i := strings.Index(content, key)
		if i < 0 {
			t.Fatalf("key %q is missing:\n%s", key, content)
		}
		if i < last {
			t.Errorf("keys are not in sorted order:\n%s", content)
		}
		last = i
	}
}

func TestParsePathAndFormatPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		want    string // round-tripped through formatPath
		wantErr string
	}{
		{name: "simple", path: "decoded.a.b", want: "decoded.a.b"},
		{name: "index", path: "decoded.a[0].b", want: "decoded.a[0].b"},
		{name: "nested arrays", path: "decoded.a[0][1].b", want: "decoded.a[0][1].b"},
		{name: "last index", path: "decoded.a[0]", want: "decoded.a[0]"},
		{name: "escaped dot", path: `decoded.a\.b`, want: `decoded.a\.b`},
		{name: "escaped colon", path: `decoded.a\:b`, want: `decoded.a\:b`},
		{name: "escaped brackets", path: `decoded.a\[0\]`, want: `decoded.a\[0\]`},
		{name: "escape sequences", path: `decoded.a\nb`, want: `decoded.a\nb`},
		{name: "escaped backslash", path: `decoded.a\\b`, want: `decoded.a\\b`},
		{name: "multi byte rune after a redundant escape", path: `decoded.a\é`, want: `decoded.aé`},
		{name: "empty segment", path: "decoded..a", wantErr: "empty segment"},
		{name: "leading dot", path: ".a", wantErr: "empty segment"},
		{name: "trailing escape", path: `decoded.a\`, wantErr: "ends with an escape"},
		{name: "unterminated index", path: "decoded.a[0", wantErr: "unterminated index"},
		{name: "invalid index", path: "decoded.a[x]", wantErr: "invalid index"},
		{name: "negative index", path: "decoded.a[-1]", wantErr: "invalid index"},
		{name: "unmatched bracket", path: "decoded.a]", wantErr: "unmatched"},
		{name: "unexpected character", path: "decoded.a[0]b", wantErr: "unexpected"},
		{name: "empty", path: "", wantErr: "is empty"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			segs, err := parsePath(tc.path)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("parsePath(%q) accepted the path", tc.path)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parsePath(%q) error = %v", tc.path, err)
			}
			if got := formatPath(segs); got != tc.want {
				t.Errorf("formatPath(parsePath(%q)) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestEscapePathKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{in: "plain", want: "plain"},
		{in: "a.b", want: `a\.b`},
		{in: "a:b", want: `a\:b`},
		{in: "a[0]", want: `a\[0\]`},
		{in: `a\b`, want: `a\\b`},
		{in: "a\nb", want: `a\nb`},
		{in: "a\rb", want: `a\rb`},
		{in: "a\tb", want: `a\tb`},
	}

	for _, tc := range tests {
		if got := escapePathKey(tc.in); got != tc.want {
			t.Errorf("escapePathKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFlattenTree(t *testing.T) {
	t.Parallel()

	tree, err := decodeTree([]byte(`{"a":{"b":[1,{"c":null}]},"empty":{},"emptyList":[],"x":"yes"}`))
	if err != nil {
		t.Fatalf("decodeTree() error = %v", err)
	}

	want := []string{
		`decoded.a.b[0]: 1`,
		`decoded.a.b[1].c: null`,
		`decoded.empty: {}`,
		`decoded.emptyList: []`,
		`decoded.x: "yes"`,
	}
	if got := flattenTree("decoded", tree); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("flattenTree() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestBuildTreeRejectsAmbiguousPaths(t *testing.T) {
	t.Parallel()

	key := func(k string) pathSegment { return pathSegment{key: k} }
	idx := func(i int) pathSegment { return pathSegment{index: i, isIdx: true} }

	tests := []struct {
		name    string
		entries []flatEntry
		wantErr string
	}{
		{
			name: "index mixed into a mapping",
			entries: []flatEntry{
				{segs: []pathSegment{key("a")}, val: "1"},
				{segs: []pathSegment{idx(0)}, val: "2"},
			},
			wantErr: "mixes an array index into a mapping",
		},
		{
			name: "key mixed into an array",
			entries: []flatEntry{
				{segs: []pathSegment{idx(0)}, val: "1"},
				{segs: []pathSegment{key("a")}, val: "2"},
			},
			wantErr: "mixes a mapping key into an array",
		},
		{
			name: "duplicate leaf",
			entries: []flatEntry{
				{segs: []pathSegment{key("a")}, val: "1"},
				{segs: []pathSegment{key("a")}, val: "2"},
			},
			wantErr: "duplicate path a",
		},
		{
			name: "value then container",
			entries: []flatEntry{
				{segs: []pathSegment{key("a")}, val: "1"},
				{segs: []pathSegment{key("a"), key("b")}, val: "2"},
			},
			wantErr: "both a value and a container",
		},
		{
			name: "container then value",
			entries: []flatEntry{
				{segs: []pathSegment{key("a"), key("b")}, val: "2"},
				{segs: []pathSegment{key("a")}, val: "1"},
			},
			wantErr: "both a value and a container",
		},
		{
			name: "array value then container",
			entries: []flatEntry{
				{segs: []pathSegment{key("a"), idx(0)}, val: "1"},
				{segs: []pathSegment{key("a"), idx(0), key("b")}, val: "2"},
			},
			wantErr: "both a value and a container",
		},
		{
			name: "duplicate array index",
			entries: []flatEntry{
				{segs: []pathSegment{key("a"), idx(0)}, val: "1"},
				{segs: []pathSegment{key("a"), idx(0)}, val: "2"},
			},
			wantErr: "duplicate path a[0]",
		},
		{
			name: "gap in an array",
			entries: []flatEntry{
				{segs: []pathSegment{key("a"), idx(0)}, val: "1"},
				{segs: []pathSegment{key("a"), idx(2)}, val: "3"},
			},
			wantErr: "array index 1 is missing",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tree, err := buildTree(tc.entries)
			if err == nil {
				t.Fatalf("buildTree() = %#v, want an error", tree)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestBuildTreeAssemblesNestedValues(t *testing.T) {
	t.Parallel()

	entries := []flatEntry{
		{segs: []pathSegment{{key: "a"}, {key: "b"}}, val: "1"},
		{segs: []pathSegment{{key: "a"}, {key: "c"}, {key: "d"}}, val: nil},
		{segs: []pathSegment{{key: "list"}, {index: 0, isIdx: true}}, val: "x"},
		{segs: []pathSegment{{key: "list"}, {index: 1, isIdx: true}, {key: "e"}}, val: true},
		{segs: []pathSegment{{key: "grid"}, {index: 0, isIdx: true}, {index: 0, isIdx: true}},
			val: json.Number("2")},
	}

	tree, err := buildTree(entries)
	if err != nil {
		t.Fatalf("buildTree() error = %v", err)
	}
	raw, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshalling the tree: %v", err)
	}

	const want = `{"a":{"b":"1","c":{"d":null}},"grid":[[2]],"list":["x",{"e":true}]}`
	if string(raw) != want {
		t.Errorf("buildTree() = %s, want %s", raw, want)
	}
}

func TestTreeHelpers(t *testing.T) {
	t.Parallel()

	t.Run("describeTree names every kind", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			in   any
			want string
		}{
			{in: nil, want: "null"},
			{in: map[string]any{}, want: "a mapping"},
			{in: []any{}, want: "a sequence"},
			{in: "s", want: "a string"},
			{in: true, want: "a boolean"},
			{in: json.Number("1"), want: "a number"},
			{in: 7, want: "int"},
		}
		for _, tc := range tests {
			if got := describeTree(tc.in); got != tc.want {
				t.Errorf("describeTree(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		}
	})

	t.Run("treeString", func(t *testing.T) {
		t.Parallel()

		obj := map[string]any{"good": "value", "bad": 7, "empty": nil}
		if got, err := treeString(obj, "good"); err != nil || got != "value" {
			t.Errorf("treeString(good) = (%q, %v)", got, err)
		}
		if got, err := treeString(obj, "missing"); err != nil || got != "" {
			t.Errorf("treeString(missing) = (%q, %v)", got, err)
		}
		if got, err := treeString(obj, "empty"); err != nil || got != "" {
			t.Errorf("treeString(empty) = (%q, %v)", got, err)
		}
		if _, err := treeString(obj, "bad"); err == nil {
			t.Error("treeString accepted a non-string")
		} else if !strings.Contains(err.Error(), "bad must be a string") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("treeUint32", func(t *testing.T) {
		t.Parallel()

		if got, err := treeUint32(json.Number("3966820")); err != nil || got != 3966820 {
			t.Errorf("treeUint32() = (%d, %v)", got, err)
		}
		if _, err := treeUint32(json.Number("4294967296")); err == nil {
			t.Error("treeUint32 accepted a value past 32 bits")
		}
		if _, err := treeUint32(json.Number("-1")); err == nil {
			t.Error("treeUint32 accepted a negative value")
		}
		if _, err := treeUint32("1"); err == nil {
			t.Error("treeUint32 accepted a string")
		} else if !strings.Contains(err.Error(), "must be a number") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("provenanceFromTree", func(t *testing.T) {
		t.Parallel()

		if got, err := provenanceFromTree(nil); err != nil || got != nil {
			t.Errorf("provenanceFromTree(nil) = (%v, %v)", got, err)
		}
		if _, err := provenanceFromTree("testnet"); err == nil {
			t.Error("provenanceFromTree accepted a string")
		}

		got, err := provenanceFromTree(map[string]any{
			"network": "testnet",
			"ledger":  json.Number("7"),
			"txHash":  "ab",
			"at":      "2026-08-05T12:00:00Z",
		})
		if err != nil {
			t.Fatalf("provenanceFromTree() error = %v", err)
		}
		want := &Provenance{Network: "testnet", Ledger: 7, TxHash: "ab", At: "2026-08-05T12:00:00Z"}
		if *got != *want {
			t.Errorf("provenanceFromTree() = %+v, want %+v", *got, *want)
		}

		for _, bad := range []map[string]any{
			{"network": 7},
			{"txHash": 7},
			{"at": 7},
			{"endpoint": 7},
			{"ledger": "7"},
		} {
			if _, err := provenanceFromTree(bad); err == nil {
				t.Errorf("provenanceFromTree(%v) accepted a bad field", bad)
			}
		}
	})

	t.Run("fixtureFromTree", func(t *testing.T) {
		t.Parallel()

		if _, err := fixtureFromTree("not a mapping"); err == nil {
			t.Error("fixtureFromTree accepted a scalar")
		} else if !strings.Contains(err.Error(), "top level must be a mapping") {
			t.Errorf("error = %v", err)
		}

		for _, key := range []string{"format", "type", "description", "xdr"} {
			if _, err := fixtureFromTree(map[string]any{key: 7}); err == nil {
				t.Errorf("fixtureFromTree accepted a non-string %s", key)
			}
		}

		fixture, err := fixtureFromTree(map[string]any{
			"format":  FormatVersion,
			"type":    "TransactionEnvelope",
			"xdr":     "AAAA",
			"decoded": map[string]any{"a": json.Number("1")},
		})
		if err != nil {
			t.Fatalf("fixtureFromTree() error = %v", err)
		}
		if string(fixture.Decoded) != `{"a":1}` {
			t.Errorf("Decoded = %s, want {\"a\":1}", fixture.Decoded)
		}

		// A decoded section that cannot be re-encoded is a fixture error, not a
		// silent omission.
		if _, err := fixtureFromTree(map[string]any{
			"format": FormatVersion, "xdr": "AAAA", "decoded": math.Inf(1),
		}); err == nil {
			t.Error("fixtureFromTree accepted an unencodable decoded section")
		}
	})
}

func TestErrorfNamesTheLine(t *testing.T) {
	t.Parallel()

	p := &yamlParser{lines: []yamlLine{{indent: 0, text: "a: 1", num: 12}}}
	err := p.errorf(p.cur(), "something went wrong: %d", 7)
	if err == nil {
		t.Fatal("errorf returned nil")
	}
	if !strings.Contains(err.Error(), "line 12") || !strings.Contains(err.Error(), "7") {
		t.Errorf("errorf() = %v", err)
	}
}

func TestDecodeTreeKeepsNumbersExact(t *testing.T) {
	t.Parallel()

	tree, err := decodeTree([]byte(`{"big":9223372036854775807}`))
	if err != nil {
		t.Fatalf("decodeTree() error = %v", err)
	}
	obj, ok := tree.(map[string]any)
	if !ok {
		t.Fatalf("decodeTree() = %T, want a mapping", tree)
	}
	if got, ok := obj["big"].(json.Number); !ok || string(got) != "9223372036854775807" {
		t.Errorf("a large integer was rounded: %#v", obj["big"])
	}

	if _, err := decodeTree([]byte("{")); err == nil {
		t.Error("decodeTree accepted malformed JSON")
	}
}

func TestSplitTextLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		in        string
		wantPath  string
		wantValue string
		wantOK    bool
	}{
		{name: "pair", in: "decoded.a: 1", wantPath: "decoded.a", wantValue: "1", wantOK: true},
		{name: "escaped colon in the path", in: `decoded.a\:b: "x: y"`,
			wantPath: `decoded.a\:b`, wantValue: `"x: y"`, wantOK: true},
		{name: "no separator", in: "decoded.a", wantOK: false},
		{name: "trailing escape", in: `decoded.a\`, wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path, value, ok := splitTextLine(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("splitTextLine(%q) ok = %v, want %v", tc.in, ok, tc.wantOK)
			}
			if ok && (path != tc.wantPath || value != tc.wantValue) {
				t.Errorf("splitTextLine(%q) = (%q, %q), want (%q, %q)",
					tc.in, path, value, tc.wantPath, tc.wantValue)
			}
		})
	}
}
