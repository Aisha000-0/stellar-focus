package capture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/stellar/go-stellar-sdk/xdr"
)

// ContractWASMSource serves contract code ledger entries.
//
// It is Source narrowed to what a WASM capture needs, so a test can drive the
// capture from a canned entry and nothing here opens a connection. Any Source
// satisfies it.
type ContractWASMSource interface {
	// Name identifies the source in messages, e.g. "soroban-rpc".
	Name() string
	// Network reports which network the source is pointed at.
	Network() string
	// Endpoint reports the URL, for provenance.
	Endpoint() string
	// LedgerEntry fetches one entry by its base64 LedgerKey.
	LedgerEntry(ctx context.Context, key string) ([]Record, error)
}

// ErrContractCodeHashMismatch is returned when a deployed contract's stored
// WASM does not hash to the code hash the ledger records for it.
//
// The two are meant to agree: the hash in the entry is what every reference
// to the contract is keyed on, so a mismatch means the bytes are not the
// bytes the ledger is talking about — a truncated capture, or a source that
// re-encoded them. Reporting it rather than returning the bytes keeps a
// corrupt archive from looking like a valid one.
var ErrContractCodeHashMismatch = errors.New("contract code does not hash to its recorded code hash")

// ErrNotContractCode is returned when a ledger entry turns out not to hold
// contract code at all.
//
// Asking for a contract and getting back an account or a config setting is a
// wrong answer, not an empty one: reporting an empty WASM would make the
// capture look like a deployed contract with no body.
var ErrNotContractCode = errors.New("ledger entry does not hold contract code")

// ContractWASM is one captured deployed contract.
type ContractWASM struct {
	// CodeHash is the code hash the ledger stores for this entry, hex
	// encoded. It is what a contract ID derived from deploying this WASM
	// equals.
	CodeHash string
	// Code is the raw WASM bytes, unmodified.
	Code []byte
	// Ledger is the ledger the entry was read at, when the source reported
	// one.
	Ledger uint32
}

// ContractCodeKey encodes the ledger key that names a deployed contract's
// WASM, base64 encoded as getLedgerEntries expects it.
//
// The contract ID is the 32-byte hex hash the contract is addressed by. An ID
// that is not 32 hex bytes is an error rather than a best-effort key: sending
// a malformed key to an endpoint gets back "not found", which would read as
// "this contract is not deployed" and send a capture looking for the wrong
// thing.
func ContractCodeKey(contractID string) (string, error) {
	raw, err := hex.DecodeString(contractID)
	if err != nil {
		return "", fmt.Errorf("decoding contract id %q: %w", contractID, err)
	}
	if len(raw) != len(xdr.Hash{}) {
		return "", fmt.Errorf("contract id %q is %d bytes, want %d", contractID, len(raw), len(xdr.Hash{}))
	}

	var hash xdr.Hash
	copy(hash[:], raw)

	var key xdr.LedgerKey
	if err := key.SetContractCode(hash); err != nil {
		return "", fmt.Errorf("building contract code ledger key: %w", err)
	}

	encoded, err := key.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("encoding contract code ledger key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(encoded), nil
}

// FetchContractWASM fetches the WASM a deployed contract runs and turns it
// into a capture record.
//
// The record carries the whole LedgerEntryData rather than only the code
// bytes, so re-fetching and re-parsing it later checks the entry the network
// returns rather than a projection of it. The code itself stays reachable
// through ParseContractWASM for assertions written against the bytes.
func FetchContractWASM(ctx context.Context, src ContractWASMSource, contractID string) (Record, error) {
	key, err := ContractCodeKey(contractID)
	if err != nil {
		return Record{}, err
	}

	records, err := src.LedgerEntry(ctx, key)
	if err != nil {
		return Record{}, fmt.Errorf("ledger entry for contract %s: %w", contractID, err)
	}
	if len(records) == 0 {
		return Record{}, fmt.Errorf("ledger entry for contract %s: %w", contractID, ErrNotFound)
	}

	record := records[0]
	contract, err := ParseContractWASM(record.XDR)
	if err != nil {
		return Record{}, fmt.Errorf("contract %s: %w", contractID, err)
	}

	// The ledger addresses contract code by the hash of its bytes, so the
	// stored hash naming the same bytes is the cheapest integrity check
	// available before anything is written down.
	if got := sha256.Sum256(contract.Code); hex.EncodeToString(got[:]) != contract.CodeHash {
		return Record{}, fmt.Errorf("contract %s: %w: code is %d bytes, hashed to %x, ledger says %s",
			contractID, ErrContractCodeHashMismatch, len(contract.Code), got, contract.CodeHash)
	}

	return Record{
		Name:        "wasm",
		Type:        "ContractCode",
		XDR:         record.XDR,
		Ledger:      record.Ledger,
		TxHash:      contract.CodeHash,
		Description: fmt.Sprintf("deployed contract %s (%s of WASM)", contractID, wasmSize(len(contract.Code))),
	}, nil
}

// wasmSize describes a code size the way a fixture reader can check it
// without counting bytes by eye.
func wasmSize(size int) string {
	return fmt.Sprintf("%d bytes", size)
}

// ParseContractWASM decodes a base64 LedgerEntryData — the shape
// getLedgerEntries returns — into the code hash and WASM bytes a capture
// asserts on.
//
// An entry that is not contract code is an error, not an empty ContractWASM:
// asking for a contract and getting back an account entry would otherwise
// report an empty WASM as if the contract had been deployed without one.
func ParseContractWASM(b64 string) (ContractWASM, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ContractWASM{}, fmt.Errorf("decoding contract code entry: %w", err)
	}

	var data xdr.LedgerEntryData
	if _, err := xdr.Unmarshal(bytes.NewReader(raw), &data); err != nil {
		return ContractWASM{}, fmt.Errorf("parsing contract code entry: %w", err)
	}

	code := data.GetContractCode()
	if code == nil {
		return ContractWASM{}, fmt.Errorf("ledger entry is %s, not contract code: %w",
			data.Type, ErrNotContractCode)
	}

	return ContractWASM{
		CodeHash: hex.EncodeToString(code.Hash[:]),
		Code:     code.Code,
	}, nil
}
