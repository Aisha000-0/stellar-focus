package focus_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-focus/internal/faketb"
	"github.com/stellar-optics/stellar-focus/pkg/focus"
)

// inflationResult builds a successful transaction result with one inflation
// operation paying out the given payouts.
func inflationResult(t *testing.T, payouts ...xdr.InflationPayout) string {
	t.Helper()

	return buildResult(t, xdr.TransactionResultCodeTxSuccess, xdr.OperationResult{
		Code: xdr.OperationResultCodeOpInner,
		Tr: &xdr.OperationResultTr{
			Type: xdr.OperationTypeInflation,
			InflationResult: &xdr.InflationResult{
				Code:    xdr.InflationResultCodeInflationSuccess,
				Payouts: &payouts,
			},
		},
	})
}

// inflationNotTimeResult builds a result for the dormant-inflation outcome of
// current networks.
func inflationNotTimeResult(t *testing.T) string {
	t.Helper()

	return buildResult(t, xdr.TransactionResultCodeTxSuccess, xdr.OperationResult{
		Code: xdr.OperationResultCodeOpInner,
		Tr: &xdr.OperationResultTr{
			Type:            xdr.OperationTypeInflation,
			InflationResult: &xdr.InflationResult{Code: xdr.InflationResultCodeInflationNotTime},
		},
	})
}

func payout(t *testing.T, addr string, amount int64) xdr.InflationPayout {
	t.Helper()
	return xdr.InflationPayout{Destination: mustAccountID(t, addr), Amount: xdr.Int64(amount)}
}

// ledgerHeader builds a minimal ledger header carrying a fee pool.
func ledgerHeaderValue(seq, feePool int64) xdr.LedgerHeader {
	return xdr.LedgerHeader{
		LedgerSeq: xdr.Uint32(seq),
		FeePool:   xdr.Int64(feePool),
	}
}

func TestAssertInflationPayouts(t *testing.T) {
	t.Parallel()

	result := inflationResult(t,
		payout(t, testSource, 1_0000000),
		payout(t, testDest, 2_0000000),
	)

	t.Run("passes for the exact list", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertInflationPayouts(tb, result, []focus.InflationPayout{
			{Destination: testSource, Amount: 1_0000000},
			{Destination: testDest, Amount: 2_0000000},
		}) {
			t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
		}
	})

	t.Run("passes for an empty want on not-time", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertInflationPayouts(tb, inflationNotTimeResult(t), nil) {
			t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
		}
	})

	t.Run("fails on not-time with payouts wanted", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertInflationPayouts(tb, inflationNotTimeResult(t), []focus.InflationPayout{
			{Destination: testSource, Amount: 1},
		}) {
			t.Fatal("assertion passed for a payout that did not happen")
		}
		if !strings.Contains(tb.Output(), "inflation did not run") {
			t.Errorf("message does not explain the not-time result:\n%s", tb.Output())
		}
	})

	t.Run("fails on a wrong amount and shows both lists", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertInflationPayouts(tb, result, []focus.InflationPayout{
			{Destination: testSource, Amount: 9_0000000},
			{Destination: testDest, Amount: 2_0000000},
		}) {
			t.Fatal("assertion passed for a wrong payout")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		for _, want := range []string{
			"inflation paid 2 payout(s), want 2",
			"payouts made (2)",
			"payouts wanted (2)",
			"10000000 (1.0000000 XLM)",
			"90000000 (9.0000000 XLM)",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})

	t.Run("fails when no inflation operation ran", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		result := buildResult(t, xdr.TransactionResultCodeTxSuccess, paymentResult(xdr.PaymentResultCodePaymentSuccess))
		if focus.AssertInflationPayouts(tb, result, nil) {
			t.Fatal("assertion passed for a payment result")
		}
		if !strings.Contains(tb.Output(), "no inflation operation result") {
			t.Errorf("message does not say the operation was absent:\n%s", tb.Output())
		}
	})

	t.Run("fails when the transaction failed", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		result := buildResult(t, xdr.TransactionResultCodeTxFailed, paymentResult(xdr.PaymentResultCodePaymentUnderfunded))
		if focus.AssertInflationPayouts(tb, result, nil) {
			t.Fatal("assertion passed for a failed transaction")
		}
		if !strings.Contains(tb.Output(), "the transaction failed") {
			t.Errorf("message does not say the transaction failed:\n%s", tb.Output())
		}
	})
}

func TestAssertInflationPaid(t *testing.T) {
	t.Parallel()

	result := inflationResult(t,
		payout(t, testSource, 1_0000000),
		payout(t, testDest, 2_0000000),
		payout(t, testSource, 3_0000000),
	)

	t.Run("passes for a single payout", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertInflationPaid(tb, result, testDest, 2_0000000) {
			t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
		}
	})

	t.Run("passes for the sum of repeated payouts", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertInflationPaid(tb, result, testSource, 4_0000000) {
			t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
		}
	})

	t.Run("fails for an account with no payout", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertInflationPaid(tb, result, testIssuer, 1) {
			t.Fatal("assertion passed for an account with no payout")
		}
		if !strings.Contains(tb.Output(), "made no payout to GA2JR…HVAW") {
			t.Errorf("message does not name the account:\n%s", tb.Output())
		}
	})

	t.Run("fails for a wrong total", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertInflationPaid(tb, result, testSource, 1) {
			t.Fatal("assertion passed for a wrong total")
		}
		if !strings.Contains(tb.Output(), "paid GCJNH…ZST6 a total of 40000000 (4.0000000 XLM)") {
			t.Errorf("message does not state the total paid:\n%s", tb.Output())
		}
	})

	t.Run("not-time result", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertInflationPaid(tb, inflationNotTimeResult(t), testSource, 1) {
			t.Fatal("assertion passed for a not-time result")
		}
		if !strings.Contains(tb.Output(), "inflation did not run") {
			t.Errorf("message does not explain the not-time result:\n%s", tb.Output())
		}
	})
}

func TestAssertFeePool(t *testing.T) {
	t.Parallel()

	header := ledgerHeaderValue(1234, 987_0000000)

	t.Run("xdr header", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertFeePool(tb, header, 987_0000000) {
			t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
		}
	})

	t.Run("base64 header", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertFeePool(tb, mustMarshal(t, header), 987_0000000) {
			t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
		}
	})

	t.Run("failure names the ledger", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertFeePool(tb, header, 1) {
			t.Fatal("assertion passed for a wrong fee pool")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		for _, want := range []string{
			"ledger 1234 fee pool is 9870000000 (987.0000000 XLM)",
			"want 1 (0.0000001 XLM)",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})
}

func TestAssertFeePoolChanged(t *testing.T) {
	t.Parallel()

	before := ledgerHeaderValue(100, 1000_0000000)
	after := ledgerHeaderValue(101, 990_0000000)

	t.Run("passes for a drawdown", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertFeePoolChanged(tb, before, after, -10_0000000) {
			t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
		}
	})

	t.Run("passes for fees collected", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertFeePoolChanged(tb, before, ledgerHeaderValue(101, 1001_0000000), 1_0000000) {
			t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
		}
	})

	t.Run("fails for a wrong delta", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertFeePoolChanged(tb, before, after, 10_0000000) {
			t.Fatal("assertion passed for a wrong delta")
		}
		if !strings.Contains(tb.Output(), "a change of -100000000 (-10.0000000 XLM), want 100000000 (10.0000000 XLM)") {
			t.Errorf("message does not state both deltas:\n%s", tb.Output())
		}
	})

	t.Run("fails for reversed headers", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertFeePoolChanged(tb, after, before, 10_0000000) {
			t.Fatal("assertion passed for reversed headers")
		}
		if !strings.Contains(tb.Output(), "headers are out of order") {
			t.Errorf("message does not report the ordering:\n%s", tb.Output())
		}
	})
}
