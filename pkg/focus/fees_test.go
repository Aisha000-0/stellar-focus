package focus_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-focus/internal/faketb"
	"github.com/stellar-optics/stellar-focus/pkg/focus"
)

// buildResultWithFee returns a base64 TransactionResult charged the given fee,
// so tests can vary the one field the fee assertions read.
func buildResultWithFee(t *testing.T, fee int64, code xdr.TransactionResultCode, ops ...xdr.OperationResult) string {
	t.Helper()

	res := xdr.TransactionResult{
		FeeCharged: xdr.Int64(fee),
		Result:     xdr.TransactionResultResult{Code: code},
	}
	if code == xdr.TransactionResultCodeTxFailed || code == xdr.TransactionResultCodeTxSuccess {
		results := ops
		res.Result.Results = &results
	}
	return mustMarshal(t, res)
}

// buildSorobanMetaFees wraps an explicit resource fee breakdown in a V3
// transaction meta.
func buildSorobanMetaFees(t *testing.T, nonRefundable, refundable, rent int64) string {
	t.Helper()

	return mustMarshal(t, xdr.TransactionMeta{
		V: 3,
		V3: &xdr.TransactionMetaV3{
			SorobanMeta: &xdr.SorobanTransactionMeta{
				ReturnValue: xdr.ScVal{Type: xdr.ScValTypeScvVoid},
				Ext: xdr.SorobanTransactionMetaExt{
					V: 1,
					V1: &xdr.SorobanTransactionMetaExtV1{
						TotalNonRefundableResourceFeeCharged: xdr.Int64(nonRefundable),
						TotalRefundableResourceFeeCharged:    xdr.Int64(refundable),
						RentFeeCharged:                       xdr.Int64(rent),
					},
				},
			},
		},
	})
}

func TestAssertConsumedFeeWithin(t *testing.T) {
	t.Parallel()

	failed := buildResultWithFee(t, 35_438, xdr.TransactionResultCodeTxFailed,
		paymentResult(xdr.PaymentResultCodePaymentUnderfunded))

	t.Run("within the ceiling", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertConsumedFeeWithin(tb, failed, 40_000) {
			t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
		}
	})

	t.Run("exactly on the ceiling", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertConsumedFeeWithin(tb, failed, 35_438) {
			t.Errorf("assertion failed on an exactly equal ceiling:\n%s", tb.Output())
		}
	})

	t.Run("over the ceiling reports the overrun", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertConsumedFeeWithin(tb, failed, 30_000) {
			t.Fatal("assertion passed when the fee was over the ceiling")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		for _, want := range []string{
			"AssertConsumedFeeWithin",
			"fee charged is 35438",
			"want at most 30000",
			"over by 5438",
			"XLM",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})

	t.Run("bad result input", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertConsumedFeeWithin(tb, nil, 1) {
			t.Fatal("assertion passed for a nil result")
		}
		msg, _ := tb.Message()
		if !strings.Contains(msg, "value could not be read") {
			t.Errorf("message does not describe the input problem:\n%s", msg)
		}
	})
}

func TestAssertRefundFeeEqual(t *testing.T) {
	t.Parallel()

	meta := buildSorobanMetaFees(t, 4_000, 12_345, 1_234)

	t.Run("matching refundable fee", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertRefundFeeEqual(tb, meta, 12_345) {
			t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
		}
	})

	t.Run("zero refundable fee", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertRefundFeeEqual(tb, buildSorobanMetaFees(t, 4_000, 0, 0), 0) {
			t.Errorf("assertion failed for a failed transaction:\n%s", tb.Output())
		}
	})

	t.Run("mismatch shows the whole breakdown", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertRefundFeeEqual(tb, meta, 10_000) {
			t.Fatal("assertion passed with the wrong refundable fee")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		for _, want := range []string{
			"AssertRefundFeeEqual",
			"refundable resource fee is 12345",
			"want 10000",
			"non-refundable resource fee: 4000",
			"of which rent:             1234",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})
}

func TestAssertRefundFeeEqualMetaProblems(t *testing.T) {
	t.Parallel()

	classic := mustMarshal(t, xdr.TransactionMeta{V: 0, Operations: &[]xdr.OperationMeta{}})
	noExt := mustMarshal(t, xdr.TransactionMeta{
		V: 3,
		V3: &xdr.TransactionMetaV3{
			SorobanMeta: &xdr.SorobanTransactionMeta{
				ReturnValue: xdr.ScVal{Type: xdr.ScValTypeScvVoid},
			},
		},
	})

	tests := []struct {
		name     string
		meta     focus.Value
		contains []string
	}{
		{
			name:     "classic meta has no Soroban section",
			meta:     classic,
			contains: []string{"no Soroban section"},
		},
		{
			name:     "Soroban meta with no fee extension",
			meta:     noExt,
			contains: []string{"no resource fee breakdown"},
		},
		{
			name:     "nil meta",
			meta:     nil,
			contains: []string{"value could not be read"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if focus.AssertRefundFeeEqual(tb, tc.meta, 1) {
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
