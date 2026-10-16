package focus_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-focus/internal/faketb"
	"github.com/stellar-optics/stellar-focus/pkg/focus"
)

// Fixed windows rather than the clock: an assertion that used time.Now would
// be flaky and would not prove the bounds are read from the envelope.
const (
	testMinTime int64 = 1_760_000_000
	testMaxTime int64 = 1_760_000_300
)

func TestAssertTimeboundsValidPasses(t *testing.T) {
	t.Parallel()

	timeOnly := envelopeValue(t, paymentOp(t, 100))
	timeOnly.V1.Tx.Cond = precondTime(&xdr.TimeBounds{
		MinTime: xdr.TimePoint(testMinTime),
		MaxTime: xdr.TimePoint(testMaxTime),
	})

	v2 := envelopeValue(t, paymentOp(t, 100))
	v2.V1.Tx.Cond = precondV2(&xdr.TimeBounds{
		MinTime: xdr.TimePoint(testMinTime),
		MaxTime: xdr.TimePoint(testMaxTime),
	}, &xdr.LedgerBounds{MinLedger: 100, MaxLedger: 200})

	noUpper := envelopeValue(t, paymentOp(t, 100))
	noUpper.V1.Tx.Cond = precondTime(&xdr.TimeBounds{
		MinTime: xdr.TimePoint(testMinTime),
		MaxTime: 0,
	})

	zeroBounds := envelopeValue(t, paymentOp(t, 100))
	zeroBounds.V1.Tx.Cond = precondTime(&xdr.TimeBounds{MinTime: 0, MaxTime: 0})

	tests := []struct {
		name     string
		envelope xdr.TransactionEnvelope
		min      int64
		max      int64
	}{
		{"no preconditions declares no bounds", envelopeValue(t, paymentOp(t, 100)), 0, 0},
		{"time-only preconditions", timeOnly, testMinTime, testMaxTime},
		{"v2 preconditions", v2, testMinTime, testMaxTime},
		{"no upper bound", noUpper, testMinTime, 0},
		{"explicit zero bounds read as none", zeroBounds, 0, 0},
		{
			"v0 envelope time bounds",
			v0EnvelopeValue(t, &xdr.TimeBounds{
				MinTime: xdr.TimePoint(testMinTime),
				MaxTime: xdr.TimePoint(testMaxTime),
			}, paymentOp(t, 100)),
			testMinTime, testMaxTime,
		},
		{
			"fee bump reads the inner transaction's bounds",
			feeBumpEnvelopeValue(t, timeOnly, nil, nil),
			testMinTime, testMaxTime,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if !focus.AssertTimeboundsValid(tb, tc.envelope, tc.min, tc.max) {
				t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
			}
		})
	}
}

// TestAssertTimeboundsValidMessages checks that a mismatch names the bound that
// changed and shows it both as a timestamp and as raw seconds, because
// transactions are built with one and debugged with the other.
func TestAssertTimeboundsValidMessages(t *testing.T) {
	t.Parallel()

	bounded := func(min, max int64) xdr.TransactionEnvelope {
		env := envelopeValue(t, paymentOp(t, 100))
		env.V1.Tx.Cond = precondTime(&xdr.TimeBounds{
			MinTime: xdr.TimePoint(min),
			MaxTime: xdr.TimePoint(max),
		})
		return env
	}

	wantMax := formatUnix(testMaxTime + 60)
	gotMax := formatUnix(testMaxTime)
	wantMin := formatUnix(testMinTime - 60)
	gotMin := formatUnix(testMinTime)

	tests := []struct {
		name     string
		envelope xdr.TransactionEnvelope
		min      int64
		max      int64
		contains []string
	}{
		{
			name:     "max time differs",
			envelope: bounded(testMinTime, testMaxTime),
			min:      testMinTime,
			max:      testMaxTime + 60,
			contains: []string{
				"AssertTimeboundsValid: time bounds differ",
				"max time is 1760000300 (" + gotMax + ")",
				"want 1760000360 (" + wantMax + ")",
				"until " + gotMax,
			},
		},
		{
			name:     "min time differs",
			envelope: bounded(testMinTime, testMaxTime),
			min:      testMinTime - 60,
			max:      testMaxTime,
			contains: []string{
				"min time is 1760000000 (" + gotMin + ")",
				"want 1759999940 (" + wantMin + ")",
			},
		},
		{
			name:     "no bounds declared",
			envelope: envelopeValue(t, paymentOp(t, 100)),
			min:      testMinTime,
			max:      testMaxTime,
			contains: []string{
				"got   no time bounds (valid indefinitely)",
				"min time is 0 (unbounded)",
				"max time is 0 (unbounded)",
			},
		},
		{
			name:     "bounds declared but none wanted",
			envelope: bounded(testMinTime, testMaxTime),
			min:      0,
			max:      0,
			contains: []string{
				"got   valid from " + formatUnix(testMinTime) + " until " + formatUnix(testMaxTime),
				"want  no time bounds (valid indefinitely)",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if focus.AssertTimeboundsValid(tb, tc.envelope, tc.min, tc.max) {
				t.Fatal("assertion passed when it should have failed")
			}
			msg, err := tb.Message()
			if err != nil {
				t.Fatalf("%v", err)
			}
			for _, want := range tc.contains {
				if !strings.Contains(msg, want) {
					t.Errorf("message does not contain %q:\n%s", want, msg)
				}
			}
		})
	}
}

func TestAssertLedgerboundsValidPasses(t *testing.T) {
	t.Parallel()

	bounded := envelopeValue(t, paymentOp(t, 100))
	bounded.V1.Tx.Cond = precondV2(nil, &xdr.LedgerBounds{MinLedger: 100, MaxLedger: 200})

	noUpper := envelopeValue(t, paymentOp(t, 100))
	noUpper.V1.Tx.Cond = precondV2(nil, &xdr.LedgerBounds{MinLedger: 42, MaxLedger: 0})

	tests := []struct {
		name     string
		envelope xdr.TransactionEnvelope
		min      uint32
		max      uint32
	}{
		{"no bounds declared", envelopeValue(t, paymentOp(t, 100)), 0, 0},
		{"v2 ledger bounds", bounded, 100, 200},
		{"no upper bound", noUpper, 42, 0},
		{"fee bump reads the inner transaction's bounds", feeBumpEnvelopeValue(t, bounded, nil, nil), 100, 200},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if !focus.AssertLedgerboundsValid(tb, tc.envelope, tc.min, tc.max) {
				t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
			}
		})
	}
}

func TestAssertLedgerboundsValidMessages(t *testing.T) {
	t.Parallel()

	bounded := func(min, max uint32) xdr.TransactionEnvelope {
		env := envelopeValue(t, paymentOp(t, 100))
		env.V1.Tx.Cond = precondV2(nil, &xdr.LedgerBounds{MinLedger: xdr.Uint32(min), MaxLedger: xdr.Uint32(max)})
		return env
	}

	tests := []struct {
		name     string
		envelope xdr.TransactionEnvelope
		min      uint32
		max      uint32
		contains []string
	}{
		{
			name:     "max ledger differs",
			envelope: bounded(100, 200),
			min:      100,
			max:      300,
			contains: []string{
				"AssertLedgerboundsValid: ledger bounds differ",
				"ledger bounds 100..200",
				"max ledger is 200, want 300",
			},
		},
		{
			name:     "min ledger differs",
			envelope: bounded(100, 200),
			min:      50,
			max:      200,
			contains: []string{"min ledger is 100, want 50"},
		},
		{
			name:     "no bounds declared",
			envelope: envelopeValue(t, paymentOp(t, 100)),
			min:      100,
			max:      200,
			contains: []string{
				"got   no ledger bounds",
				"min ledger is 0 (unbounded)",
				"max ledger is 0 (unbounded)",
			},
		},
		{
			name:     "one-sided bound rendered as unbounded",
			envelope: bounded(42, 0),
			min:      42,
			max:      99,
			contains: []string{
				"valid from ledger 42 (no upper bound)",
				"max ledger is 0 (unbounded)",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if focus.AssertLedgerboundsValid(tb, tc.envelope, tc.min, tc.max) {
				t.Fatal("assertion passed when it should have failed")
			}
			msg, err := tb.Message()
			if err != nil {
				t.Fatalf("%v", err)
			}
			for _, want := range tc.contains {
				if !strings.Contains(msg, want) {
					t.Errorf("message does not contain %q:\n%s", want, msg)
				}
			}
		})
	}
}

// TestPreconditionAssertionsOnBadInput checks that a non-envelope input fails
// with the standard input message rather than a confusing bounds diff.
func TestPreconditionAssertionsOnBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(focus.TB) bool
	}{
		{"timebounds", func(tb focus.TB) bool { return focus.AssertTimeboundsValid(tb, 42, 0, 0) }},
		{"ledgerbounds", func(tb focus.TB) bool { return focus.AssertLedgerboundsValid(tb, 42, 0, 0) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if tc.call(tb) {
				t.Fatal("assertion passed on a non-envelope value")
			}
			msg, err := tb.Message()
			if err != nil {
				t.Fatalf("%v", err)
			}
			if !strings.Contains(msg, "envelope value could not be read") {
				t.Errorf("message does not name the bad input:\n%s", msg)
			}
		})
	}
}

// formatUnix renders a test timestamp in the form the failure messages use.
func formatUnix(sec int64) string {
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}
