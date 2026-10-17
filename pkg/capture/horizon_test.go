package capture

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHorizonSourceTransaction(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/transactions/abc123" {
			t.Fatalf("path = %q, want /transactions/abc123", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"envelope_xdr":"env","result_xdr":"res","result_meta_xdr":"meta","ledger":123}`))
	}))
	defer srv.Close()

	src := NewHorizonSource(NetworkTestnet, srv.URL, time.Second)
	got, err := src.Transaction(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("Transaction() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(Transaction()) = %d, want 3", len(got))
	}
	if got[0].Type != "TransactionEnvelope" || got[1].Type != "TransactionResult" || got[2].Type != "TransactionMeta" {
		t.Fatalf("unexpected record types: %#v", got)
	}
}

func TestHorizonSourceLedgerEntry(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ledger_entries" {
			t.Fatalf("path = %q, want /ledger_entries", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"records":[{"data_xdr":"entry","last_modified_ledger":42}]}`))
	}))
	defer srv.Close()

	src := NewHorizonSource(NetworkTestnet, srv.URL, time.Second)
	got, err := src.LedgerEntry(context.Background(), "key")
	if err != nil {
		t.Fatalf("LedgerEntry() error = %v", err)
	}
	if len(got) != 1 || got[0].Type != "LedgerEntryData" || got[0].Ledger != 42 {
		t.Fatalf("unexpected ledger entry: %#v", got)
	}
}
