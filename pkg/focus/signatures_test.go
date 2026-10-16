package focus_test

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-focus/internal/faketb"
	"github.com/stellar-optics/stellar-focus/pkg/focus"
)

// decoratedSignature builds a signature whose hint is the hint the protocol
// derives for the signer, which is the only link an envelope carries between a
// signature and the key that made it.
func decoratedSignature(t *testing.T, addr string) xdr.DecoratedSignature {
	t.Helper()
	return xdr.DecoratedSignature{
		Hint:      signerHint(t, addr),
		Signature: xdr.Signature(bytes.Repeat([]byte{0x5a}, 64)),
	}
}

// signerHint derives the four-byte hint for any signer strkey.
func signerHint(t *testing.T, addr string) xdr.SignatureHint {
	t.Helper()

	vb, err := strkey.Version(addr)
	if err != nil {
		t.Fatalf("reading the version of %s: %v", addr, err)
	}
	raw, err := strkey.Decode(vb, addr)
	if err != nil {
		t.Fatalf("decoding %s: %v", addr, err)
	}

	var hint xdr.SignatureHint
	if vb == strkey.VersionByteSignedPayload {
		// A signed payload's hint comes from the ed25519 key inside it.
		copy(hint[:], raw[28:32])
		return hint
	}
	copy(hint[:], raw[len(raw)-4:])
	return hint
}

func hintHex(t *testing.T, addr string) string {
	t.Helper()
	hint := signerHint(t, addr)
	return hex.EncodeToString(hint[:])
}

func signedEnvelope(t *testing.T, addrs ...string) xdr.TransactionEnvelope {
	t.Helper()

	env := envelopeValue(t, paymentOp(t, 100))
	for _, addr := range addrs {
		env.V1.Signatures = append(env.V1.Signatures, decoratedSignature(t, addr))
	}
	return env
}

func TestAssertSignaturesPresentPasses(t *testing.T) {
	t.Parallel()

	preAuth := signerAddressOfVersion(t, strkey.VersionByteHashTx)
	hashX := signerAddressOfVersion(t, strkey.VersionByteHashX)
	signedPayload := signedPayloadAddress(t)

	tests := []struct {
		name     string
		envelope xdr.TransactionEnvelope
		keys     []focus.Value
	}{
		{
			"the source signed",
			signedEnvelope(t, testSource),
			[]focus.Value{testSource},
		},
		{
			"every expected signer signed",
			signedEnvelope(t, testSource, testDest),
			[]focus.Value{testSource, testDest},
		},
		{
			"unexpected extra signatures are fine",
			signedEnvelope(t, testIssuer, testSource, testDest),
			[]focus.Value{testSource, testDest},
		},
		{
			"an xdr.AccountId expectation",
			signedEnvelope(t, testDest),
			[]focus.Value{mustAccountID(t, testDest)},
		},
		{
			"an xdr.MuxedAccount expectation",
			signedEnvelope(t, testSource),
			[]focus.Value{mustMuxed(t, testSource)},
		},
		{
			"an xdr.SignerKey expectation",
			signedEnvelope(t, testDest),
			[]focus.Value{xdr.MustSigner(testDest)},
		},
		{
			"a pre-auth-tx signer",
			signedEnvelope(t, preAuth),
			[]focus.Value{preAuth},
		},
		{
			"a hash-x signer",
			signedEnvelope(t, hashX),
			[]focus.Value{hashX},
		},
		{
			"a signed-payload signer",
			signedEnvelope(t, signedPayload),
			[]focus.Value{signedPayload},
		},
		{
			"a fee-bump signature counts",
			feeBumpEnvelopeValue(t,
				envelopeValue(t, paymentOp(t, 100)),
				[]xdr.DecoratedSignature{decoratedSignature(t, testDest)},
				nil),
			[]focus.Value{testDest},
		},
		{
			"an inner signature counts",
			feeBumpEnvelopeValue(t,
				envelopeValue(t, paymentOp(t, 100)),
				nil,
				[]xdr.DecoratedSignature{decoratedSignature(t, testSource)}),
			[]focus.Value{testSource},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if !focus.AssertSignaturesPresent(tb, tc.envelope, tc.keys...) {
				t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
			}
		})
	}
}

// TestAssertSignaturesPresentMessages checks that a missing signature names
// the signer it was expected from and shows the signatures that are there, so
// a hint that belongs to nobody expected is visible.
func TestAssertSignaturesPresentMessages(t *testing.T) {
	t.Parallel()

	missingHint := hintHex(t, testDest)
	presentHint := hintHex(t, testSource)

	tests := []struct {
		name     string
		envelope xdr.TransactionEnvelope
		keys     []focus.Value
		contains []string
	}{
		{
			name:     "one signer did not sign",
			envelope: signedEnvelope(t, testSource),
			keys:     []focus.Value{testSource, testDest},
			contains: []string{
				"AssertSignaturesPresent: 1 of 2 expected signer(s) have no matching signature",
				"no signature",
				"signed (inner [0])",
				"hint " + missingHint,
				"hint " + presentHint,
			},
		},
		{
			name:     "no signatures at all",
			envelope: envelopeValue(t, paymentOp(t, 100)),
			keys:     []focus.Value{testSource},
			contains: []string{
				"signatures carried: none",
				"GCJNH…ZST6",
			},
		},
		{
			name: "the signature set is named",
			envelope: feeBumpEnvelopeValue(t,
				envelopeValue(t, paymentOp(t, 100)),
				[]xdr.DecoratedSignature{decoratedSignature(t, testDest)},
				nil),
			keys: []focus.Value{testDest, testSource},
			contains: []string{
				"signed (fee-bump [0])",
				"fee-bump hint " + hintHex(t, testDest),
			},
		},
		{
			name:     "no expected keys",
			envelope: signedEnvelope(t, testSource),
			keys:     nil,
			contains: []string{
				"no expected public keys were given",
			},
		},
		{
			name:     "unreadable expected key",
			envelope: signedEnvelope(t, testSource),
			keys:     []focus.Value{"not-a-strkey"},
			contains: []string{
				"expectedPubKeys[0] could not be read as a signer",
			},
		},
		{
			name:     "unsupported expected key type",
			envelope: signedEnvelope(t, testSource),
			keys:     []focus.Value{42},
			contains: []string{
				"expectedPubKeys[0] could not be read as a signer",
				"unsupported type int",
			},
		},
		{
			name:     "non-envelope value",
			envelope: xdr.TransactionEnvelope{},
			keys:     []focus.Value{testSource},
			contains: []string{
				"envelope value could not be read",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			value := focus.Value(tc.envelope)
			if tc.name == "non-envelope value" {
				value = 42
			}
			if focus.AssertSignaturesPresent(tb, value, tc.keys...) {
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

// TestAssertSignatureThreshold covers the M-of-N case where the individual
// signers are not fixed: any M of the listed keys satisfies the threshold.
func TestAssertSignatureThreshold(t *testing.T) {
	t.Parallel()

	twoOfThree := signedEnvelope(t, testSource, testDest)
	oneOfThree := signedEnvelope(t, testSource)
	allThree := signedEnvelope(t, testSource, testDest, testIssuer)
	keys := []focus.Value{testSource, testDest, testIssuer}

	t.Run("two of three signs", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertSignatureThreshold(tb, twoOfThree, 2, keys...) {
			t.Errorf("2-of-3 threshold not met:\n%s", tb.Output())
		}
	})

	t.Run("one of three does not meet two", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertSignatureThreshold(tb, oneOfThree, 2, keys...) {
			t.Fatal("threshold passed with too few signatures")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		for _, want := range []string{
			"AssertSignatureThreshold: only 1 of 3 expected signer(s) signed, want at least 2",
			"signed (inner [0])",
			"no signature",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})

	t.Run("threshold equal to the signer count is the all-of check", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertSignatureThreshold(tb, allThree, 3, keys...) {
			t.Errorf("3-of-3 threshold not met:\n%s", tb.Output())
		}
	})

	t.Run("a threshold below one is an input error", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertSignatureThreshold(tb, allThree, 0, keys...) {
			t.Fatal("threshold 0 passed")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.Contains(msg, "threshold is 0; at least 1 expected signer must sign") {
			t.Errorf("message does not explain the bad threshold:\n%s", msg)
		}
	})

	t.Run("a threshold above the signer count is an input error", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertSignatureThreshold(tb, allThree, 4, keys...) {
			t.Fatal("threshold above the signer count passed")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !strings.Contains(msg, "threshold is 4 but only 3 expected signer(s) were given") {
			t.Errorf("message does not explain the bad threshold:\n%s", msg)
		}
	})
}

// signerAddressOfVersion builds a T… or X… signer address, whose hint comes
// from the end of the hash rather than a public key.
func signerAddressOfVersion(t *testing.T, version strkey.VersionByte) string {
	t.Helper()

	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i + 1)
	}
	addr, err := strkey.Encode(version, hash)
	if err != nil {
		t.Fatalf("encoding signer address: %v", err)
	}
	return addr
}

// signedPayloadAddress builds a P… signer address from the test source key.
func signedPayloadAddress(t *testing.T) string {
	t.Helper()

	payload, err := strkey.NewSignedPayload(testSource, []byte("receipt"))
	if err != nil {
		t.Fatalf("building signed payload: %v", err)
	}
	addr, err := payload.Encode()
	if err != nil {
		t.Fatalf("encoding signed payload: %v", err)
	}
	return addr
}
