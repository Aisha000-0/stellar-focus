package focus

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

// The typed memo assertions take the memo's value in the form the caller
// already holds it — a string for a text memo, a uint64 for an id, bytes for a
// hash — instead of the "text: …" display form AssertMemo compares.
//
// They exist alongside AssertMemo because the display form makes a wrong memo
// type read like a wrong value: "id: 42" against "43" hides that the
// transaction carries the wrong kind of memo entirely. These name the type the
// transaction actually has, which is the part that is usually wrong.
//
// A memo is a union: at most one of text, id, hash and return is set. Asking
// for a text memo on a transaction with an id memo is a normal failure, not an
// input error, and the failure says which memo it found.

// AssertMemoTextEqual checks that the transaction carries a text memo with
// exactly this text.
//
// The protocol caps a text memo at 28 bytes, so a longer want can never match
// and the failure says so rather than leaving the reader to discover the cap.
func AssertMemoTextEqual(t TB, envelope Value, want string) bool {
	t.Helper()
	const assertion = "AssertMemoTextEqual"

	memo, decoded, ok := resolveMemo(t, assertion, envelope)
	if !ok {
		return false
	}
	if memo.Type != xdr.MemoTypeMemoText {
		t.Errorf("%s", memoMismatch(assertion, memo, decoded, describeWantedText(want)))
		return false
	}

	var (
		got  string
		read bool
	)
	if !guard(t, assertion, func() { got, read = memo.GetText() }) {
		return false
	}
	if !read {
		t.Errorf("%s: the text memo could not be read; its XDR is malformed", assertion)
		return false
	}
	if got == want {
		return true
	}

	msg := mismatchMessage(assertion, "text memo", want, got)
	if len(want) > maxTextMemoBytes {
		msg += fmt.Sprintf("\n(a text memo holds at most %d bytes)", maxTextMemoBytes)
	}
	t.Errorf("%s", msg)
	return false
}

// AssertMemoIDEqual checks that the transaction carries an id memo with
// exactly this id. Id memos hold a uint64.
func AssertMemoIDEqual(t TB, envelope Value, want uint64) bool {
	t.Helper()
	const assertion = "AssertMemoIDEqual"

	memo, decoded, ok := resolveMemo(t, assertion, envelope)
	if !ok {
		return false
	}
	if memo.Type != xdr.MemoTypeMemoId {
		t.Errorf("%s", memoMismatch(assertion, memo, decoded, fmt.Sprintf("an id memo %d", want)))
		return false
	}

	var (
		got  xdr.Uint64
		read bool
	)
	if !guard(t, assertion, func() { got, read = memo.GetId() }) {
		return false
	}
	if !read {
		t.Errorf("%s: the id memo could not be read; its XDR is malformed", assertion)
		return false
	}
	if uint64(got) == want {
		return true
	}
	t.Errorf("%s", mismatchMessage(assertion, "id memo", want, uint64(got)))
	return false
}

// AssertMemoHashEqual checks that the transaction carries a hash memo with
// exactly these 32 bytes.
//
// want accepts an xdr.Hash, a [32]byte, 32 raw bytes, or the hash as 64 hex
// characters, in either case.
func AssertMemoHashEqual(t TB, envelope Value, want Value) bool {
	t.Helper()
	return assertMemoHashEqual(t, "AssertMemoHashEqual", envelope, want, xdr.MemoTypeMemoHash)
}

// AssertMemoReturnEqual checks that the transaction carries a return memo with
// exactly these 32 bytes, the memo form a refund or rejection receipt uses.
//
// It accepts the same forms as AssertMemoHashEqual.
func AssertMemoReturnEqual(t TB, envelope Value, want Value) bool {
	t.Helper()
	return assertMemoHashEqual(t, "AssertMemoReturnEqual", envelope, want, xdr.MemoTypeMemoReturn)
}

// assertMemoHashEqual is the shared implementation behind the hash and return
// memo assertions, whose values are both 32-byte hashes.
func assertMemoHashEqual(t TB, assertion string, envelope, want Value, kind xdr.MemoType) bool {
	t.Helper()

	wantHash, err := hashFrom(want)
	if err != nil {
		t.Errorf("%s: want could not be read as a %s: %v", assertion, memoKind(kind), err)
		return false
	}

	memo, decoded, ok := resolveMemo(t, assertion, envelope)
	if !ok {
		return false
	}
	if memo.Type != kind {
		t.Errorf("%s", memoMismatch(assertion, memo, decoded,
			fmt.Sprintf("a %s %s", memoKind(kind), wantHash.HexString())))
		return false
	}

	var (
		got  xdr.Hash
		read bool
	)
	if !guard(t, assertion, func() { got, read = memoHash(memo, kind) }) {
		return false
	}
	if !read {
		t.Errorf("%s: the %s could not be read; its XDR is malformed", assertion, memoKind(kind))
		return false
	}
	if got == wantHash {
		return true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s memo differs\n\n", assertion, memoKind(kind))
	fmt.Fprintf(&b, "  got   %s\n", got.HexString())
	fmt.Fprintf(&b, "  want  %s", wantHash.HexString())
	t.Errorf("%s", b.String())
	return false
}

// memoHash reads the hash arm a hash or return memo carries. It calls
// generated accessors, so it must run behind guard.
func memoHash(memo xdr.Memo, kind xdr.MemoType) (xdr.Hash, bool) {
	if kind == xdr.MemoTypeMemoReturn {
		return memo.GetRetHash()
	}
	return memo.GetHash()
}

// resolveMemo reads an envelope's memo together with its decoded form, which
// the failure messages explain so the actual memo is rendered exactly as
// `lens explain` renders it.
func resolveMemo(t TB, assertion string, envelope Value) (xdr.Memo, *lens.Value, bool) {
	t.Helper()

	env, decoded, ok := envelopeFrom(t, assertion, envelope)
	if !ok {
		return xdr.Memo{}, nil, false
	}

	var memo xdr.Memo
	if !guard(t, assertion, func() { memo = env.Memo() }) {
		return xdr.Memo{}, nil, false
	}
	return memo, decoded, true
}

// memoMismatch renders the failure for a memo of the wrong type, naming both
// what the transaction carries and what the assertion wanted. A memo that is
// not set is described as unset rather than as an empty memo.
func memoMismatch(assertion string, memo xdr.Memo, decoded *lens.Value, want string) string {
	return fmt.Sprintf("%s: %s, want %s", assertion, describeMemoFor(memo, decoded), want)
}

// describeMemoFor renders the memo a transaction carries for a failure
// message, such as "the memo is an id memo (id: 42)". The parenthesised value
// comes from lens, so it matches what `lens explain` prints.
func describeMemoFor(memo xdr.Memo, decoded *lens.Value) string {
	if memo.Type == xdr.MemoTypeMemoNone {
		return "the memo is unset"
	}

	out := fmt.Sprintf("the memo is %s memo", memoArticle(memoKind(memo.Type)))
	if display := explainedMemo(decoded); display != "" {
		out += " (" + display + ")"
	}
	return out
}

// memoArticle gives a memo kind its indefinite article, which is "an" for the
// id memo and "a" for the rest.
func memoArticle(kind string) string {
	if kind == "id" {
		return "an " + kind
	}
	return "a " + kind
}

// explainedMemo returns lens's rendering of an envelope's memo, e.g.
// "text: hello" or "id: 42". It returns "" when the value cannot be
// summarised.
//
// A panic is swallowed rather than reported: the rendering is only used to
// make an already-failing assertion friendlier, and reporting a second failure
// for it would fail the same test twice for one problem.
func explainedMemo(decoded *lens.Value) (out string) {
	defer func() {
		if recover() != nil {
			out = ""
		}
	}()

	summary, err := lens.Explain(decoded)
	if err != nil || summary == nil {
		return ""
	}
	return summary.Memo
}

// memoKind names a memo type as a noun phrase, so it reads in a sentence.
func memoKind(t xdr.MemoType) string {
	switch t {
	case xdr.MemoTypeMemoNone:
		return "unset"
	case xdr.MemoTypeMemoText:
		return "text"
	case xdr.MemoTypeMemoId:
		return "id"
	case xdr.MemoTypeMemoHash:
		return "hash"
	case xdr.MemoTypeMemoReturn:
		return "return"
	default:
		return fmt.Sprintf("type %d", t)
	}
}

// hashFrom accepts the forms a caller may hold a 32-byte memo hash in.
func hashFrom(v Value) (xdr.Hash, error) {
	switch val := v.(type) {
	case xdr.Hash:
		return val, nil
	case *xdr.Hash:
		if val == nil {
			return xdr.Hash{}, fmt.Errorf("value is a nil *xdr.Hash")
		}
		return *val, nil
	case [32]byte:
		return xdr.Hash(val), nil
	case []byte:
		if len(val) != len(xdr.Hash{}) {
			return xdr.Hash{}, fmt.Errorf("hash is %d byte(s), want 32", len(val))
		}
		var h xdr.Hash
		copy(h[:], val)
		return h, nil
	case string:
		raw, err := hex.DecodeString(strings.TrimSpace(val))
		if err != nil {
			return xdr.Hash{}, fmt.Errorf("%q is not hex: %w", v, err)
		}
		if len(raw) != len(xdr.Hash{}) {
			return xdr.Hash{}, fmt.Errorf("hash is %d byte(s), want 32 (64 hex characters)", len(raw))
		}
		var h xdr.Hash
		copy(h[:], raw)
		return h, nil
	default:
		return xdr.Hash{}, fmt.Errorf(
			"unsupported type %T; expected an xdr.Hash, a [32]byte, 32 raw bytes or 64 hex characters", v)
	}
}

// describeWantedText names the text memo an assertion expected. A want longer
// than the protocol's cap can never match, so the description says that rather
// than leaving the reader to wonder why a text that is "in" the transaction
// failed.
func describeWantedText(want string) string {
	out := "a text memo " + render(want)
	if len(want) > maxTextMemoBytes {
		out += fmt.Sprintf(" (a text memo holds at most %d bytes)", maxTextMemoBytes)
	}
	return out
}

// maxTextMemoBytes is the protocol's cap on a text memo.
const maxTextMemoBytes = 28
