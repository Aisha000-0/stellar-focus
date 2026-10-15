package focus_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-focus/internal/faketb"
	"github.com/stellar-optics/stellar-focus/pkg/focus"
)

// operationResultWithCode builds a bare operation result carrying an
// operation-level code, as opposed to an opINNER result.
func operationResultWithCode(code xdr.OperationResultCode) xdr.OperationResult {
	return xdr.OperationResult{Code: code}
}

func TestAssertOperationResultCodePasses(t *testing.T) {
	t.Parallel()

	mixed := buildResult(t, xdr.TransactionResultCodeTxFailed,
		paymentResult(xdr.PaymentResultCodePaymentSuccess),
		paymentResult(xdr.PaymentResultCodePaymentUnderfunded))
	succeeded := buildResult(t, xdr.TransactionResultCodeTxSuccess,
		paymentResult(xdr.PaymentResultCodePaymentSuccess))
	wrapperCode := buildResult(t, xdr.TransactionResultCodeTxFailed,
		operationResultWithCode(xdr.OperationResultCodeOpBadAuth))

	tests := []struct {
		name   string
		result string
		index  int
		want   string
	}{
		{"failure code on a failed operation", mixed, 1, "payment_underfunded"},
		{"success code on a successful operation", mixed, 0, "payment_success"},
		{"success code inside a failed transaction", mixed, 0, "payment_success"},
		{"operation-level wrapper code", wrapperCode, 0, "op_bad_auth"},
		{"success code in a successful transaction", succeeded, 0, "payment_success"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if !focus.AssertOperationResultCode(tb, tc.result, tc.index, tc.want) {
				t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
			}
			if tb.Failed() {
				t.Errorf("passing assertion reported a failure:\n%s", tb.Output())
			}
		})
	}
}

// TestAssertOperationResultCodeFailureMessages checks the messages for the two
// ways this assertion fails: the operation answered with a different code, and
// the expected code could never match because it is not a real short code.
func TestAssertOperationResultCodeFailureMessages(t *testing.T) {
	t.Parallel()

	failed := buildResult(t, xdr.TransactionResultCodeTxFailed,
		paymentResult(xdr.PaymentResultCodePaymentSuccess),
		paymentResult(xdr.PaymentResultCodePaymentNoTrust))

	tests := []struct {
		name     string
		result   focus.Value
		index    int
		want     string
		contains []string
	}{
		{
			name:     "wrong code explains both sides",
			result:   failed,
			index:    1,
			want:     "payment_underfunded",
			contains: []string{"operation 1", "payment_no_trust", `want "payment_underfunded"`, "trustline"},
		},
		{
			name:     "typo is named as an unknown code",
			result:   failed,
			index:    1,
			want:     "paymnt_underfunded",
			contains: []string{"not a known result code", "short forms", "paymnt_underfunded"},
		},
		{
			name:     "xdr constant is named as the wrong form",
			result:   failed,
			index:    1,
			want:     "PaymentResultCodePaymentUnderfunded",
			contains: []string{"XDR constant", "payment_underfunded"},
		},
		{
			name:     "index out of range names the transaction size",
			result:   failed,
			index:    7,
			want:     "payment_success",
			contains: []string{"no operation at index 7", "2 operation(s)"},
		},

		{
			name:     "bad result input names the argument",
			result:   nil,
			index:    0,
			want:     "payment_success",
			contains: []string{"AssertOperationResultCode", "value could not be read"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if focus.AssertOperationResultCode(tb, tc.result, tc.index, tc.want) {
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
