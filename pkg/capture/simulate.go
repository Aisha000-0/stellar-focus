package capture

import (
	"context"
	"errors"
	"fmt"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
)

// SimulationSource serves simulateTransaction responses.
//
// Like Source, it is a seam: the network-backed implementation calls a real
// RPC endpoint, and tests substitute a canned response so a capture can be
// built and asserted on offline.
type SimulationSource interface {
	// Network reports which network the source is pointed at.
	Network() string
	// Endpoint reports the URL, for provenance.
	Endpoint() string
	// SimulateTransaction runs one simulateTransaction request.
	SimulateTransaction(ctx context.Context, req protocol.SimulateTransactionRequest) (*protocol.SimulateTransactionResponse, error)
}

// ErrSimulationFailed is returned when the RPC accepted the request but the
// host rejected the invocation: the response carries an error instead of a
// result. The cause says where the host stopped, which is exactly what a
// preflight wants to know.
var ErrSimulationFailed = errors.New("simulation failed")

// Simulation is one captured simulateTransaction response.
type Simulation struct {
	// Resp is the RPC's own response, unmodified, including its error field
	// when Simulate passed it through for inspection.
	Resp *protocol.SimulateTransactionResponse
}

// Simulate runs one preflight simulation and records it.
//
// A host-rejected invocation is reported as an error wrapping
// ErrSimulationFailed rather than returned as an unmarked success: a capture
// whose result is an error string is still information, but the caller must
// handle both branches explicitly, and the error chain carries the host's
// message for that.
func Simulate(ctx context.Context, src SimulationSource, req protocol.SimulateTransactionRequest) (*Simulation, error) {
	resp, err := src.SimulateTransaction(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("simulateTransaction: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("simulateTransaction: endpoint returned no response")
	}
	if resp.Error != "" {
		return &Simulation{Resp: resp}, fmt.Errorf("simulateTransaction: %w: %s", ErrSimulationFailed, resp.Error)
	}
	return &Simulation{Resp: resp}, nil
}

// Records splits the response into capture records, one per XDR payload, so
// each can become its own fixture.
//
// The resource footprint and resource fee live inside the SorobanTransactionData
// record, so capturing that one record captures both. The authorization tree
// comes back as one base64 ContractAuth per required address, each its own
// record, which is what "authorization tree" means in the RPC response: a
// list, not a single structure.
func (s *Simulation) Records() []Record {
	if s == nil || s.Resp == nil {
		return nil
	}
	resp := s.Resp
	ledger := resp.LatestLedger

	context := fmt.Sprintf("simulation against %s (min resource fee %d)", ledgerName(ledger), resp.MinResourceFee)
	record := func(name, typ, xdr string) Record {
		return Record{
			Name:        name,
			Type:        typ,
			XDR:         xdr,
			Ledger:      ledger,
			Description: context,
		}
	}

	var out []Record
	if resp.TransactionDataXDR != "" {
		out = append(out,
			record("tx-data", "SorobanTransactionData", resp.TransactionDataXDR))
	}
	for i, ev := range resp.EventsXDR {
		out = append(out, record(fmt.Sprintf("event-%d", i+1), "DiagnosticEvent", ev))
	}
	for op, res := range resp.Results {
		if res.AuthXDR != nil {
			for j, a := range *res.AuthXDR {
				size := len(*res.AuthXDR)
				out = append(out, record(
					authName(op, j, size),
					"ContractAuth", a))
			}
		}
		if res.ReturnValueXDR != nil && *res.ReturnValueXDR != "" {
			out = append(out, record("return", "ScVal", *res.ReturnValueXDR))
		}
	}
	return out
}

// ledgerName labels the ledger a simulation ran against, only for
// descriptions; zero means the endpoint did not report one.
func ledgerName(ledger uint32) string {
	if ledger == 0 {
		return "unknown ledger"
	}
	return fmt.Sprintf("ledger %d", ledger)
}

// authName names one authorization record within one host function result:
// the RPC returns an authorization list per host function, so the name says
// which tree and which entry it is.
func authName(op, idx, size int) string {
	if size == 1 {
		return fmt.Sprintf("auth-%d", op)
	}
	return fmt.Sprintf("auth-%d-%d", op, idx)
}

// SimulateTransaction implements SimulationSource against the real endpoint.
//
// It lives on RPCSource rather than a separate type so one configured source
// serves both transaction history, ledger entries and preflight simulations.
func (s *RPCSource) SimulateTransaction(ctx context.Context, req protocol.SimulateTransactionRequest) (*protocol.SimulateTransactionResponse, error) {
	resp, err := s.client.SimulateTransaction(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("rpc simulateTransaction: %w", err)
	}
	return &resp, nil
}
