package focus_test

import (
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	rpcproto "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-focus/internal/faketb"
	"github.com/stellar-optics/stellar-focus/pkg/focus"
)

// accountKeys builds n account ledger keys, for filling a footprint.
func accountKeys(t *testing.T, n int) []xdr.LedgerKey {
	t.Helper()

	keys := make([]xdr.LedgerKey, 0, n)
	for i := 0; i < n; i++ {
		keys = append(keys, xdr.LedgerKey{
			Type:    xdr.LedgerEntryTypeAccount,
			Account: &xdr.LedgerKeyAccount{AccountId: mustAccountID(t, testSource)},
		})
	}
	return keys
}

// sorobanTransactionData builds the transaction data a simulation returns,
// declaring the given resource usage.
func sorobanTransactionData(t *testing.T, instructions, diskRead, write uint32, readOnly, readWrite int) xdr.SorobanTransactionData {
	t.Helper()

	return xdr.SorobanTransactionData{
		Resources: xdr.SorobanResources{
			Footprint: xdr.LedgerFootprint{
				ReadOnly:  accountKeys(t, readOnly),
				ReadWrite: accountKeys(t, readWrite),
			},
			Instructions:  xdr.Uint32(instructions),
			DiskReadBytes: xdr.Uint32(diskRead),
			WriteBytes:    xdr.Uint32(write),
		},
		ResourceFee: xdr.Int64(1_000),
	}
}

func TestAssertSimulationResourcesWithinPasses(t *testing.T) {
	t.Parallel()

	data := sorobanTransactionData(t, 1_000, 2_000, 1_000, 2, 1)
	b64 := mustMarshal(t, data)
	raw, err := data.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	tests := []struct {
		name   string
		sim    focus.Value
		maxCPU uint32
		maxMem uint32
	}{
		{"within both budgets", b64, 2_000, 5_000},
		{"exactly on both budgets", b64, 1_000, 3_000},
		{"raw bytes", raw, 2_000, 5_000},
		{"xdr value", data, 2_000, 5_000},
		{"xdr pointer", &data, 2_000, 5_000},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if !focus.AssertSimulationResourcesWithin(tb, tc.sim, tc.maxCPU, tc.maxMem) {
				t.Errorf("assertion failed unexpectedly:\n%s", tb.Output())
			}
		})
	}
}

func TestAssertSimulationResourcesWithinAcceptsLensValue(t *testing.T) {
	t.Parallel()

	data := sorobanTransactionData(t, 1_000, 2_000, 1_000, 1, 0)
	_, decoded, err := focus.Encode(data, "SorobanTransactionData")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	tb := faketb.New()
	if !focus.AssertSimulationResourcesWithin(tb, decoded, 2_000, 5_000) {
		t.Errorf("a decoded *lens.Value was not accepted:\n%s", tb.Output())
	}
}

// TestAssertSimulationResourcesWithinFailureMessage checks that an overrun
// names which budget was exceeded, by how much, and what the transaction
// touches — the three things a resource regression report needs.
func TestAssertSimulationResourcesWithinFailureMessage(t *testing.T) {
	t.Parallel()

	data := sorobanTransactionData(t, 1_500, 40_000, 5_000, 3, 2)

	t.Run("CPU overrun", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertSimulationResourcesWithin(tb, data, 1_000, 100_000) {
			t.Fatal("assertion passed with an exceeded CPU budget")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		for _, want := range []string{
			"AssertSimulationResourcesWithin",
			"CPU instructions",
			"1500",
			"budget       1000",
			"OVER by 500",
			"3 read-only",
			"2 read-write",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})

	t.Run("memory overrun", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertSimulationResourcesWithin(tb, data, 10_000, 10_000) {
			t.Fatal("assertion passed with an exceeded ledger byte budget")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		for _, want := range []string{
			"ledger bytes",
			"45000",
			"OVER by 35000",
			"CPU instructions",
			"ok",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})
}

func TestAssertSimulationResourcesWithinRPCResponse(t *testing.T) {
	t.Parallel()

	data := sorobanTransactionData(t, 1_000, 2_000, 1_000, 1, 1)
	ok := rpcproto.SimulateTransactionResponse{TransactionDataXDR: mustMarshal(t, data)}

	t.Run("value response", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertSimulationResourcesWithin(tb, ok, 2_000, 5_000) {
			t.Errorf("a successful response was not accepted:\n%s", tb.Output())
		}
	})

	t.Run("pointer response", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertSimulationResourcesWithin(tb, &ok, 2_000, 5_000) {
			t.Errorf("a *response was not accepted:\n%s", tb.Output())
		}
	})
}

func TestAssertSimulationResourcesWithinResponseProblems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sim      focus.Value
		contains []string
	}{
		{
			name:     "simulation error",
			sim:      rpcproto.SimulateTransactionResponse{Error: "host invocation failed"},
			contains: []string{"simulation failed", "host invocation failed"},
		},
		{
			name:     "no transaction data",
			sim:      rpcproto.SimulateTransactionResponse{},
			contains: []string{"carries no transaction data"},
		},
		{
			name:     "malformed transaction data",
			sim:      rpcproto.SimulateTransactionResponse{TransactionDataXDR: "!!!not xdr!!!"},
			contains: []string{"simResponse", "could not be read"},
		},
		{
			name:     "nil pointer response",
			sim:      (*rpcproto.SimulateTransactionResponse)(nil),
			contains: []string{"nil *protocol.SimulateTransactionResponse"},
		},
		{
			name:     "unsupported input",
			sim:      42,
			contains: []string{"unsupported type"},
		},
		{
			// A *lens.Value whose type name says one thing but whose raw
			// value is another must be diagnosed, not panic on the cast.
			name:     "lens value with the wrong shape",
			sim:      &lens.Value{Type: "SorobanTransactionData", Raw: "not xdr"},
			contains: []string{"claims to be a SorobanTransactionData", "no Soroban transaction data"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if focus.AssertSimulationResourcesWithin(tb, tc.sim, 1_000, 1_000) {
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
