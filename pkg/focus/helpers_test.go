package focus_test

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// Real mainnet addresses, so the strkey enrichers in lens actually fire and
// the failure messages under test are the ones users will see.
const (
	testSource = "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"
	testDest   = "GAPB7YMUQ6MPQ23PTFDNBZP7HGGTZPI2DT5JJURUOIXNPLGHW2O3WYXZ"
	testIssuer = "GA2JRQOF6EA3HQWDCEDBPPMLYPJCFLDDGYZLEQGMS5SOBQIB3BAFHVAW"
)

func mustAccountID(t *testing.T, addr string) xdr.AccountId {
	t.Helper()
	var aid xdr.AccountId
	if err := aid.SetAddress(addr); err != nil {
		t.Fatalf("parsing address %s: %v", addr, err)
	}
	return aid
}

func mustMuxed(t *testing.T, addr string) xdr.MuxedAccount {
	t.Helper()
	aid := mustAccountID(t, addr)
	return aid.ToMuxedAccount()
}

// paymentOp builds a native payment of the given stroop amount.
func paymentOp(t *testing.T, amount int64) xdr.Operation {
	t.Helper()
	return xdr.Operation{Body: xdr.OperationBody{
		Type: xdr.OperationTypePayment,
		PaymentOp: &xdr.PaymentOp{
			Destination: mustMuxed(t, testDest),
			Asset:       xdr.Asset{Type: xdr.AssetTypeAssetTypeNative},
			Amount:      xdr.Int64(amount),
		},
	}}
}

// creditPaymentOp builds a payment of a credit asset, for tests that need a
// non-native asset in the diff.
func creditPaymentOp(t *testing.T, code string, amount int64) xdr.Operation {
	t.Helper()
	var asset xdr.Asset
	if err := asset.SetCredit(code, mustAccountID(t, testIssuer)); err != nil {
		t.Fatalf("building asset %s: %v", code, err)
	}
	return xdr.Operation{Body: xdr.OperationBody{
		Type: xdr.OperationTypePayment,
		PaymentOp: &xdr.PaymentOp{
			Destination: mustMuxed(t, testDest),
			Asset:       asset,
			Amount:      xdr.Int64(amount),
		},
	}}
}

// envelopeValue builds a minimal valid v1 envelope around the given
// operations.
func envelopeValue(t *testing.T, ops ...xdr.Operation) xdr.TransactionEnvelope {
	t.Helper()
	return envelopeValueSeq(t, 12345, ops...)
}

func envelopeValueSeq(t *testing.T, seq int64, ops ...xdr.Operation) xdr.TransactionEnvelope {
	t.Helper()
	return xdr.TransactionEnvelope{
		Type: xdr.EnvelopeTypeEnvelopeTypeTx,
		V1: &xdr.TransactionV1Envelope{
			Tx: xdr.Transaction{
				SourceAccount: mustMuxed(t, testSource),
				Fee:           xdr.Uint32(100 * len(ops)),
				SeqNum:        xdr.SequenceNumber(seq),
				Cond:          xdr.Preconditions{Type: xdr.PreconditionTypePrecondNone},
				Memo:          xdr.Memo{Type: xdr.MemoTypeMemoNone},
				Operations:    ops,
			},
		},
	}
}

// buildEnvelope returns a base64 envelope wrapping the given operations.
func buildEnvelope(t *testing.T, ops ...xdr.Operation) string {
	t.Helper()
	return mustMarshal(t, envelopeValue(t, ops...))
}

// buildEnvelopeSeq is buildEnvelope with an explicit sequence number, for
// tests exercising the ignore and normalize options.
func buildEnvelopeSeq(t *testing.T, seq int64, ops ...xdr.Operation) string {
	t.Helper()
	return mustMarshal(t, envelopeValueSeq(t, seq, ops...))
}

// buildResult returns a base64 TransactionResult with the given code and
// per-operation results.
func buildResult(t *testing.T, code xdr.TransactionResultCode, ops ...xdr.OperationResult) string {
	t.Helper()

	res := xdr.TransactionResult{
		FeeCharged: 100,
		Result:     xdr.TransactionResultResult{Code: code},
	}
	if code == xdr.TransactionResultCodeTxFailed || code == xdr.TransactionResultCodeTxSuccess {
		results := ops
		res.Result.Results = &results
	}
	return mustMarshal(t, res)
}

// paymentResult builds one operation result for a payment.
func paymentResult(code xdr.PaymentResultCode) xdr.OperationResult {
	return xdr.OperationResult{
		Code: xdr.OperationResultCodeOpInner,
		Tr: &xdr.OperationResultTr{
			Type:          xdr.OperationTypePayment,
			PaymentResult: &xdr.PaymentResult{Code: code},
		},
	}
}

// contractEvent builds a contract event with a symbol topic and an i128-free
// u32 payload, which is enough to exercise matching without noise.
func contractEvent(t *testing.T, topic string, data uint32) xdr.ContractEvent {
	t.Helper()

	sym := xdr.ScSymbol(topic)
	val := xdr.Uint32(data)
	var contractID xdr.ContractId
	copy(contractID[:], []byte("focus-test-contract-id-32bytes!!"))

	return xdr.ContractEvent{
		Type:       xdr.ContractEventTypeContract,
		ContractId: &contractID,
		Body: xdr.ContractEventBody{
			V: 0,
			V0: &xdr.ContractEventV0{
				Topics: []xdr.ScVal{{Type: xdr.ScValTypeScvSymbol, Sym: &sym}},
				Data:   xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &val},
			},
		},
	}
}

// buildSorobanMeta wraps events in a V3 transaction meta with a u32 return
// value of 7.
func buildSorobanMeta(t *testing.T, events ...xdr.ContractEvent) string {
	t.Helper()

	ret := xdr.Uint32(7)
	return mustMarshal(t, xdr.TransactionMeta{
		V: 3,
		V3: &xdr.TransactionMetaV3{
			SorobanMeta: &xdr.SorobanTransactionMeta{
				Events:      events,
				ReturnValue: xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &ret},
			},
		},
	})
}

// accountEntryValue builds a ledger entry holding an account with the given
// balance, thresholds and signers.
func accountEntryValue(t *testing.T, addr string, balance int64, th xdr.Thresholds, signers ...xdr.Signer) xdr.LedgerEntry {
	t.Helper()
	return xdr.LedgerEntry{
		LastModifiedLedgerSeq: 42,
		Data: xdr.LedgerEntryData{
			Type: xdr.LedgerEntryTypeAccount,
			Account: &xdr.AccountEntry{
				AccountId:  mustAccountID(t, addr),
				Balance:    xdr.Int64(balance),
				Thresholds: th,
				Signers:    signers,
			},
		},
	}
}

// trustLineAsset builds a credit trustline asset with the test issuer.
func trustLineAsset(t *testing.T, code string) xdr.TrustLineAsset {
	t.Helper()
	var asset xdr.Asset
	if err := asset.SetCredit(code, mustAccountID(t, testIssuer)); err != nil {
		t.Fatalf("building asset %s: %v", code, err)
	}
	return asset.ToTrustLineAsset()
}

// trustlineEntryValue builds a ledger entry holding a trustline.
func trustlineEntryValue(t *testing.T, addr, code string, balance, limit int64) xdr.LedgerEntry {
	t.Helper()
	return xdr.LedgerEntry{
		LastModifiedLedgerSeq: 42,
		Data: xdr.LedgerEntryData{
			Type: xdr.LedgerEntryTypeTrustline,
			TrustLine: &xdr.TrustLineEntry{
				AccountId: mustAccountID(t, addr),
				Asset:     trustLineAsset(t, code),
				Balance:   xdr.Int64(balance),
				Limit:     xdr.Int64(limit),
			},
		},
	}
}

func ledgerKeyOf(t *testing.T, entry xdr.LedgerEntry) xdr.LedgerKey {
	t.Helper()
	key, err := entry.LedgerKey()
	if err != nil {
		t.Fatalf("deriving a ledger key: %v", err)
	}
	return key
}

// signerValue builds one account signer.
func signerValue(t *testing.T, addr string, weight uint32) xdr.Signer {
	t.Helper()
	return xdr.Signer{Key: xdr.MustSigner(addr), Weight: xdr.Uint32(weight)}
}

func stateChange(entry xdr.LedgerEntry) xdr.LedgerEntryChange {
	return xdr.LedgerEntryChange{Type: xdr.LedgerEntryChangeTypeLedgerEntryState, State: &entry}
}

func createdChange(entry xdr.LedgerEntry) xdr.LedgerEntryChange {
	return xdr.LedgerEntryChange{Type: xdr.LedgerEntryChangeTypeLedgerEntryCreated, Created: &entry}
}

func updatedChange(entry xdr.LedgerEntry) xdr.LedgerEntryChange {
	return xdr.LedgerEntryChange{Type: xdr.LedgerEntryChangeTypeLedgerEntryUpdated, Updated: &entry}
}

func restoredChange(entry xdr.LedgerEntry) xdr.LedgerEntryChange {
	return xdr.LedgerEntryChange{Type: xdr.LedgerEntryChangeTypeLedgerEntryRestored, Restored: &entry}
}

func removedChange(key xdr.LedgerKey) xdr.LedgerEntryChange {
	return xdr.LedgerEntryChange{Type: xdr.LedgerEntryChangeTypeLedgerEntryRemoved, Removed: &key}
}

// metaV2 wraps per-operation changes in a version 2 transaction meta.
func metaV2(t *testing.T, ops ...[]xdr.LedgerEntryChange) string {
	t.Helper()
	return metaV2WithTx(t, nil, nil, ops...)
}

// metaV2WithTx wraps transaction-level and per-operation changes in a version
// 2 transaction meta, the shape a fee and a sequence bump arrive in.
func metaV2WithTx(t *testing.T, before, after []xdr.LedgerEntryChange, ops ...[]xdr.LedgerEntryChange) string {
	t.Helper()

	operations := make([]xdr.OperationMeta, 0, len(ops))
	for _, changes := range ops {
		operations = append(operations, xdr.OperationMeta{Changes: changes})
	}
	return mustMarshal(t, xdr.TransactionMeta{
		V: 2,
		V2: &xdr.TransactionMetaV2{
			TxChangesBefore: before,
			Operations:      operations,
			TxChangesAfter:  after,
		},
	})
}

// metaV0 wraps per-operation changes in a version 0 transaction meta.
func metaV0(t *testing.T, ops ...[]xdr.LedgerEntryChange) string {
	t.Helper()

	operations := make([]xdr.OperationMeta, 0, len(ops))
	for _, changes := range ops {
		operations = append(operations, xdr.OperationMeta{Changes: changes})
	}
	return mustMarshal(t, xdr.TransactionMeta{V: 0, Operations: &operations})
}

// metaV1 wraps transaction-level and per-operation changes in a version 1
// transaction meta.
func metaV1(t *testing.T, txChanges []xdr.LedgerEntryChange, ops ...[]xdr.LedgerEntryChange) string {
	t.Helper()

	operations := make([]xdr.OperationMeta, 0, len(ops))
	for _, changes := range ops {
		operations = append(operations, xdr.OperationMeta{Changes: changes})
	}
	return mustMarshal(t, xdr.TransactionMeta{
		V:  1,
		V1: &xdr.TransactionMetaV1{TxChanges: txChanges, Operations: operations},
	})
}

// metaV3WithTx wraps transaction-level and per-operation changes in a version
// 3 (Soroban) transaction meta.
func metaV3WithTx(t *testing.T, before, after []xdr.LedgerEntryChange, ops ...[]xdr.LedgerEntryChange) string {
	t.Helper()

	operations := make([]xdr.OperationMeta, 0, len(ops))
	for _, changes := range ops {
		operations = append(operations, xdr.OperationMeta{Changes: changes})
	}
	return mustMarshal(t, xdr.TransactionMeta{
		V: 3,
		V3: &xdr.TransactionMetaV3{
			TxChangesBefore: before,
			Operations:      operations,
			TxChangesAfter:  after,
		},
	})
}
