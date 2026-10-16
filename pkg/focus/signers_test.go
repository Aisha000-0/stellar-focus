package focus_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-focus/internal/faketb"
	"github.com/stellar-optics/stellar-focus/pkg/focus"
)

func TestAssertSignersEqual(t *testing.T) {
	t.Parallel()

	thresholds := xdr.NewThreshold(1, 0, 0, 0)
	entry := accountEntryValue(t, testSource, 100, thresholds,
		signerValue(t, testDest, 2), signerValue(t, testIssuer, 5))
	meta := metaV2(t, []xdr.LedgerEntryChange{updatedChange(entry)})
	want := map[string]uint32{testDest: 2, testIssuer: 5}

	tests := []struct {
		name    string
		meta    string
		account focus.Value
		want    map[string]uint32
	}{
		{"exact match", meta, testSource, want},
		{"xdr account id", meta, mustAccountID(t, testSource), want},
		{"muxed account", meta, mustMuxed(t, testSource), want},
		{
			name:    "no signers wanted, none present",
			meta:    metaV2(t, []xdr.LedgerEntryChange{updatedChange(accountEntryValue(t, testSource, 100, thresholds))}),
			account: testSource,
			want:    nil,
		},
		{
			name:    "a created account counts",
			meta:    metaV2(t, []xdr.LedgerEntryChange{createdChange(entry)}),
			account: testSource,
			want:    want,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if !focus.AssertSignersEqual(tb, tc.meta, tc.account, tc.want) {
				t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
			}
		})
	}
}

func TestAssertSignersEqualFailures(t *testing.T) {
	t.Parallel()

	thresholds := xdr.NewThreshold(1, 0, 0, 0)
	both := accountEntryValue(t, testSource, 100, thresholds,
		signerValue(t, testDest, 2), signerValue(t, testIssuer, 5))
	meta := metaV2(t, []xdr.LedgerEntryChange{updatedChange(both)})

	t.Run("wrong weight", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertSignersEqual(tb, meta, testSource, map[string]uint32{testDest: 3, testIssuer: 5}) {
			t.Fatal("assertion passed with the wrong weight")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		for _, want := range []string{
			"AssertSignersEqual: account GCJNH…ZST6 signers differ — 1 difference",
			"~ GAPB7…WYXZ weight is 2, want 3",
			"signers after the transaction (2)",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})

	t.Run("missing signer", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertSignersEqual(tb, meta, testSource, map[string]uint32{testIssuer: 5}) {
			t.Fatal("assertion passed with a signer missing")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.Contains(msg, "- GAPB7…WYXZ weight 2") {
			t.Errorf("message does not mark the extra signer:\n%s", msg)
		}
	})

	t.Run("unexpected signer", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		want := map[string]uint32{testDest: 2, testIssuer: 5, testSource: 1}
		if focus.AssertSignersEqual(tb, meta, testSource, want) {
			t.Fatal("assertion passed with a signer missing")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.Contains(msg, "+ GCJNH…ZST6 weight 1") {
			t.Errorf("message does not mark the missing signer:\n%s", msg)
		}
	})

	t.Run("all signers wanted but none present", func(t *testing.T) {
		t.Parallel()

		empty := metaV2(t, []xdr.LedgerEntryChange{
			updatedChange(accountEntryValue(t, testSource, 100, thresholds)),
		})
		tb := faketb.New()
		if focus.AssertSignersEqual(tb, empty, testSource, map[string]uint32{testDest: 2}) {
			t.Fatal("assertion passed with a signer missing")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.Contains(msg, "signers after the transaction: none") {
			t.Errorf("message does not show the empty signer set:\n%s", msg)
		}
	})
}

func TestAssertSignersEqualMissingEntry(t *testing.T) {
	t.Parallel()

	thresholds := xdr.NewThreshold(1, 0, 0, 0)
	other := accountEntryValue(t, testDest, 100, thresholds)

	t.Run("meta has no changes at all", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertSignersEqual(tb, metaV2(t), testSource, nil) {
			t.Fatal("assertion passed with no changes in the meta")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		for _, want := range []string{
			"AssertSignersEqual: this meta carries no account entry for GCJNH…ZST6, so its signers cannot be read from it",
			"no ledger entry change in this meta touches account GCJNH…ZST6",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})

	t.Run("meta only touches other accounts", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertSignersEqual(tb, metaV2(t, []xdr.LedgerEntryChange{updatedChange(other)}),
			testSource, nil) {
			t.Fatal("assertion passed for an untouched account")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.Contains(msg, "no ledger entry change in this meta touches account GCJNH…ZST6") {
			t.Errorf("message does not say the account was untouched:\n%s", msg)
		}
	})

	t.Run("invalid account", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertSignersEqual(tb, metaV2(t), 42, nil) {
			t.Fatal("assertion passed with an invalid account")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.Contains(msg, "account could not be read as an account") {
			t.Errorf("message does not name the invalid input:\n%s", msg)
		}
	})
}

func TestAssertThresholdsEqual(t *testing.T) {
	t.Parallel()

	entry := accountEntryValue(t, testSource, 100, xdr.NewThreshold(1, 2, 3, 4))
	meta := metaV2(t, []xdr.LedgerEntryChange{updatedChange(entry)})

	tests := []struct {
		name string
		want focus.Value
	}{
		{"focus thresholds", focus.Thresholds{Master: 1, Low: 2, Medium: 3, High: 4}},
		{"xdr thresholds", xdr.NewThreshold(1, 2, 3, 4)},
		{"array of bytes", [4]byte{1, 2, 3, 4}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if !focus.AssertThresholdsEqual(tb, meta, testSource, tc.want) {
				t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
			}
		})
	}

	t.Run("a zero-valued account", func(t *testing.T) {
		t.Parallel()

		fresh := accountEntryValue(t, testDest, 0, xdr.NewThreshold(0, 0, 0, 0))
		tb := faketb.New()
		if !focus.AssertThresholdsEqual(tb,
			metaV2(t, []xdr.LedgerEntryChange{createdChange(fresh)}),
			testDest, focus.Thresholds{}) {
			t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
		}
	})
}

func TestAssertThresholdsEqualFailures(t *testing.T) {
	t.Parallel()

	entry := accountEntryValue(t, testSource, 100, xdr.NewThreshold(1, 2, 3, 4))
	meta := metaV2(t, []xdr.LedgerEntryChange{updatedChange(entry)})

	t.Run("differing thresholds name each one", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertThresholdsEqual(tb, meta, testSource,
			focus.Thresholds{Master: 1, Low: 0, Medium: 3, High: 5}) {
			t.Fatal("assertion passed with differing thresholds")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		for _, want := range []string{
			"AssertThresholdsEqual: account GCJNH…ZST6 thresholds differ",
			"low threshold is 2, want 0",
			"high threshold is 4, want 5",
			"got  master 1  low 2  medium 3  high 4",
			"want master 1  low 0  medium 3  high 5",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})

	t.Run("want of an unsupported type", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertThresholdsEqual(tb, meta, testSource, 42) {
			t.Fatal("assertion passed with an unsupported want")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.Contains(msg, "want could not be read as thresholds: unsupported type int") {
			t.Errorf("message does not explain the want:\n%s", msg)
		}
	})

	t.Run("missing account entry", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertThresholdsEqual(tb, metaV2(t), testSource, focus.Thresholds{}) {
			t.Fatal("assertion passed with no changes in the meta")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.Contains(msg, "so its thresholds cannot be read from it") {
			t.Errorf("message does not explain the missing entry:\n%s", msg)
		}
	})
}
