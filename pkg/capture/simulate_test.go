package capture

import (
	"context"
	"errors"
	"fmt"
	"testing"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
)

// fakeSimulationSource is an offline SimulationSource standing in for the
// network, per the offline-first rule.
type fakeSimulationSource struct {
	network  string
	endpoint string
	resp     *protocol.SimulateTransactionResponse
	err      error
}

func (f *fakeSimulationSource) Network() string  { return f.network }
func (f *fakeSimulationSource) Endpoint() string { return f.endpoint }
func (f *fakeSimulationSource) SimulateTransaction(ctx context.Context, req protocol.SimulateTransactionRequest) (*protocol.SimulateTransactionResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func TestCaptureSimulationRecords(t *testing.T) {
	auth := []string{"base64ContractAuth1", "base64ContractAuth2"}
	ret := "base64ReturnValue"
	src := &fakeSimulationSource{
		network:  NetworkTestnet,
		endpoint: DefaultEndpoints[NetworkTestnet],
		resp: &protocol.SimulateTransactionResponse{
			LatestLedger:       700,
			MinResourceFee:     1234,
			TransactionDataXDR: "base64TxData",
			EventsXDR:          []string{"base64EventA", "base64EventB", "base64EventC"},
			Results: []protocol.SimulateHostFunctionResult{
				{AuthXDR: &auth, ReturnValueXDR: &ret},
			},
		},
	}

	sim, err := Simulate(context.Background(), src, protocol.SimulateTransactionRequest{Transaction: "base64Envelope"})
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}

	got := sim.Records()
	// tx-data, 3 events, 2 auths, 1 return value.
	if want := 7; len(got) != want {
		t.Fatalf("want %d records, got %d: %+v", want, len(got), got)
	}

	byName := make(map[string]Record, len(got))
	for _, r := range got {
		byName[r.Name] = r
	}

	txData, ok := byName["tx-data"]
	if !ok {
		t.Fatalf("no tx-data record in %+v", got)
	}
	if txData.Type != "SorobanTransactionData" || txData.XDR != "base64TxData" {
		t.Errorf("tx-data record wrong: %+v", txData)
	}
	if txData.Ledger != 700 || txData.TxHash != "" {
		t.Errorf("tx-data provenance wrong: ledger=%d txHash=%q", txData.Ledger, txData.TxHash)
	}
	if txData.Description == "" {
		t.Error("tx-data description must name ledger and min resource fee")
	}

	// Events are 1-based so record names match the RPC's own numbering.
	if byName["event-1"].XDR != "base64EventA" || byName["event-3"].XDR != "base64EventC" {
		t.Errorf("event records wrong: %+v %+v", byName["event-1"], byName["event-3"])
	}

	// One host function result carrying a two-entry authorization tree:
	// entries are keyed op-then-entry so the name locates a node in a
	// multi-entry tree rather than an index into one list.
	if byName["auth-0-0"].XDR != "base64ContractAuth1" || byName["auth-0-1"].XDR != "base64ContractAuth2" {
		t.Errorf("auth records wrong: %+v %+v", byName["auth-0-0"], byName["auth-0-1"])
	}
	if byName["auth-0-0"].Type != "ContractAuth" {
		t.Errorf("auth record type wrong: %q", byName["auth-0-0"].Type)
	}
}

func TestCaptureSimulationFailedReportsHostError(t *testing.T) {
	src := &fakeSimulationSource{
		resp: &protocol.SimulateTransactionResponse{
			Error: "HostError: Contract(ICall)",
		},
	}
	sim, err := Simulate(context.Background(), src, protocol.SimulateTransactionRequest{})
	if sim == nil {
		t.Fatalf("Simulate returned nil Simulation with error; want the response for inspection")
	}
	if !errors.Is(err, ErrSimulationFailed) {
		t.Fatalf("want ErrSimulationFailed, got: %v", err)
	}
	if !contains(err.Error(), "HostError: Contract(ICall)") {
		t.Errorf("error lost the host message: %v", err)
	}
	if got := sim.Records(); len(got) != 0 {
		t.Errorf("failed simulation must produce no records, got %+v", got)
	}
}

func TestCaptureSimulationNetworkError(t *testing.T) {
	src := &fakeSimulationSource{err: fmt.Errorf("connection refused")}
	_, err := Simulate(context.Background(), src, protocol.SimulateTransactionRequest{})
	if err == nil {
		t.Fatal("want an error on transport failure")
	}
	if errors.Is(err, ErrSimulationFailed) {
		t.Errorf("transport failure is not a host rejection: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; len(s) >= len(sub) && i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
