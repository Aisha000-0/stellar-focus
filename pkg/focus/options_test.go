package focus_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"

	"github.com/stellar-optics/stellar-focus/internal/faketb"
	"github.com/stellar-optics/stellar-focus/pkg/focus"
)

// accountLedgerKeyValue builds the ledger key of an account, for the
// footprint and key-function tests.
func accountLedgerKeyValue(tb testing.TB, addr string) xdr.LedgerKey {
	tb.Helper()

	var key xdr.LedgerKey
	if err := key.SetAccount(mustAccountID(tb, addr)); err != nil {
		tb.Fatalf("building ledger key for %s: %v", addr, err)
	}
	return key
}

// footprintValue builds Soroban transaction data whose footprint carries the
// given keys, in the order they are given.
func footprintValue(readOnly, readWrite []xdr.LedgerKey) xdr.SorobanTransactionData {
	return xdr.SorobanTransactionData{
		Resources: xdr.SorobanResources{
			Footprint: xdr.LedgerFootprint{
				ReadOnly:  readOnly,
				ReadWrite: readWrite,
			},
		},
	}
}

// TestSortPathKeyFunction covers the optional key function: it is passed each
// decoded element, controls the ordering, and falls back to canonical ordering
// when it panics.
func TestSortPathKeyFunction(t *testing.T) {
	t.Parallel()

	want := footprintValue(
		[]xdr.LedgerKey{accountLedgerKeyValue(t, testSource), accountLedgerKeyValue(t, testDest)},
		nil,
	)
	got := footprintValue(
		[]xdr.LedgerKey{accountLedgerKeyValue(t, testDest), accountLedgerKeyValue(t, testSource)},
		nil,
	)

	t.Run("the key function orders the elements", func(t *testing.T) {
		t.Parallel()

		var calls int
		keyFn := func(elem any) string {
			calls++
			n, ok := elem.(*lens.Node)
			if !ok {
				return ""
			}
			return n.Child("Account").Child("AccountId").String()
		}

		tb := faketb.New()
		if !focus.AssertEqualAs(tb, "SorobanTransactionData", want, got,
			focus.SortPath("**.Footprint.ReadOnly", keyFn)) {
			t.Errorf("the keyed sort did not treat the keys as a set:\n%s", tb.Output())
		}
		if calls == 0 {
			t.Error("the key function was never called")
		}
	})

	t.Run("a key that cannot distinguish elements does not hide the order", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertEqualAs(tb, "SorobanTransactionData", want, got,
			focus.SortPath("**.Footprint.ReadOnly", func(any) string { return "same" })) {
			t.Fatal("a constant key function made reordered keys compare equal")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.Contains(msg, "ReadOnly") {
			t.Errorf("the diff does not name the reordered array:\n%s", msg)
		}
	})

	t.Run("a panicking key function falls back to canonical order", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertEqualAs(tb, "SorobanTransactionData", want, got,
			focus.SortPath("**.Footprint.ReadOnly", func(any) string { panic("boom") })) {
			t.Errorf("a panicking key function did not fall back:\n%s", tb.Output())
		}
	})
}

// TestSortSigners treats the signer list as a set while leaving the weights
// meaningful.
func TestSortSigners(t *testing.T) {
	t.Parallel()

	thresholds := xdr.NewThreshold(1, 0, 0, 0)
	want := xdr.AccountEntry{
		AccountId:  mustAccountID(t, testSource),
		Balance:    100,
		Thresholds: thresholds,
		Signers: []xdr.Signer{
			signerValue(t, testDest, 2),
			signerValue(t, testIssuer, 5),
		},
	}
	got := want
	got.Signers = []xdr.Signer{
		signerValue(t, testIssuer, 5),
		signerValue(t, testDest, 2),
	}

	t.Run("order alone is a difference", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertEqualAs(tb, "AccountEntry", want, got) {
			t.Fatal("reordered signers compared equal without the option")
		}
	})

	t.Run("SortSigners ignores the order", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertEqualAs(tb, "AccountEntry", want, got, focus.SortSigners()) {
			t.Errorf("reordered signers were reported as a difference:\n%s", tb.Output())
		}
	})

	t.Run("a wrong weight is still a difference", func(t *testing.T) {
		t.Parallel()

		changed := got
		changed.Signers = []xdr.Signer{
			signerValue(t, testIssuer, 4),
			signerValue(t, testDest, 2),
		}

		tb := faketb.New()
		if focus.AssertEqualAs(tb, "AccountEntry", want, changed, focus.SortSigners()) {
			t.Fatal("a wrong signer weight was reported as equal")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.Contains(msg, "Weight") {
			t.Errorf("the diff does not name the changed weight:\n%s", msg)
		}
	})
}

// TestSortFootprint treats both sides of a Soroban footprint as sets.
func TestSortFootprint(t *testing.T) {
	t.Parallel()

	want := footprintValue(
		[]xdr.LedgerKey{accountLedgerKeyValue(t, testSource), accountLedgerKeyValue(t, testDest)},
		[]xdr.LedgerKey{accountLedgerKeyValue(t, testIssuer)},
	)
	got := footprintValue(
		[]xdr.LedgerKey{accountLedgerKeyValue(t, testDest), accountLedgerKeyValue(t, testSource)},
		[]xdr.LedgerKey{accountLedgerKeyValue(t, testIssuer)},
	)

	t.Run("order alone is a difference", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertEqualAs(tb, "SorobanTransactionData", want, got) {
			t.Fatal("reordered read-only keys compared equal without the option")
		}
	})

	t.Run("SortFootprint ignores both orders", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertEqualAs(tb, "SorobanTransactionData", want, got, focus.SortFootprint()) {
			t.Errorf("reordered footprint keys were reported as a difference:\n%s", tb.Output())
		}
	})

	t.Run("a missing key is still a difference", func(t *testing.T) {
		t.Parallel()

		changed := footprintValue(
			[]xdr.LedgerKey{accountLedgerKeyValue(t, testSource)},
			[]xdr.LedgerKey{accountLedgerKeyValue(t, testIssuer)},
		)

		tb := faketb.New()
		if focus.AssertEqualAs(tb, "SorobanTransactionData", want, changed, focus.SortFootprint()) {
			t.Fatal("a missing footprint key was reported as equal")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.Contains(msg, "ReadOnly") {
			t.Errorf("the diff does not name the read-only footprint:\n%s", msg)
		}
	})
}

// TestSortPathStalePatterns keeps the warning that makes sort rules
// trustworthy: a pattern that matched nothing fails the assertion even when
// the values are equal.
func TestSortPathStalePatterns(t *testing.T) {
	t.Parallel()

	entry := xdr.AccountEntry{
		AccountId:  mustAccountID(t, testSource),
		Balance:    100,
		Thresholds: xdr.NewThreshold(1, 0, 0, 0),
		Signers:    []xdr.Signer{signerValue(t, testDest, 2)},
	}

	tb := faketb.New()
	if focus.AssertEqualAs(tb, "AccountEntry", entry, entry, focus.SortPath("**.SignersTypo")) {
		t.Fatal("a sort pattern that matched nothing was accepted silently")
	}
	msg, err := tb.Message()
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(msg, "**.SignersTypo") {
		t.Errorf("the stale pattern is not named:\n%s", msg)
	}
}
