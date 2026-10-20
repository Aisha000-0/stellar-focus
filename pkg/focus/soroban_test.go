package focus_test

import (
	"strings"
	"testing"

	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/stellar-optics/stellar-focus/internal/faketb"
	"github.com/stellar-optics/stellar-focus/pkg/focus"
)

// eventWithTopics builds a contract event carrying the given symbol topics
// and a u32 payload.
//
// The sequence assertions are about order, so the events of a test have to be
// told apart: distinct topics and distinct payloads do that.
func eventWithTopics(t testing.TB, data uint32, topics ...string) xdr.ContractEvent {
	t.Helper()

	event := contractEvent(t, topics[0], data)
	body, ok := event.Body.GetV0()
	if !ok {
		t.Fatal("the event has no V0 body")
	}
	body.Topics = body.Topics[:0]
	for _, topic := range topics {
		sym := xdr.ScSymbol(topic)
		body.Topics = append(body.Topics, xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym})
	}
	return event
}

// sorobanEnvelope builds an envelope carrying Soroban transaction data with
// the given resource limits and resource fee.
func sorobanEnvelope(
	t testing.TB, instructions, readBytes, writeBytes uint32, resourceFee int64,
) xdr.TransactionEnvelope {
	t.Helper()

	env := envelopeValue(t)
	env.V1.Tx.Ext = xdr.TransactionExt{
		V: 1,
		SorobanData: &xdr.SorobanTransactionData{
			Ext: xdr.SorobanTransactionDataExt{V: 0},
			Resources: xdr.SorobanResources{
				Footprint:     xdr.LedgerFootprint{},
				Instructions:  xdr.Uint32(instructions),
				DiskReadBytes: xdr.Uint32(readBytes),
				WriteBytes:    xdr.Uint32(writeBytes),
			},
			ResourceFee: xdr.Int64(resourceFee),
		},
	}
	return env
}

func TestAssertEventsEqualPasses(t *testing.T) {
	t.Parallel()

	meta := buildSorobanMeta(t,
		eventWithTopics(t, 1, "init"),
		eventWithTopics(t, 2, "deposit"),
		eventWithTopics(t, 3, "audit"),
	)

	tests := []struct {
		name string
		want []focus.Event
		opts []focus.EventOption
	}{
		{
			name: "the whole sequence",
			want: []focus.Event{
				{Topics: []any{"init"}, Data: scU32(1)},
				{Topics: []any{"deposit"}, Data: scU32(2)},
				{Topics: []any{"audit"}, Data: scU32(3)},
			},
		},
		{
			name: "a nil topic matches any topic",
			want: []focus.Event{
				{Topics: []any{"init"}},
				{Topics: []any{nil}, Data: scU32(2)},
				{Topics: []any{"audit"}},
			},
		},
		{
			name: "a matcher with no fields matches anything",
			want: []focus.Event{{}, {}, {}},
		},
		{
			name: "the payload can be ignored",
			want: []focus.Event{
				{Topics: []any{"init"}, Data: scU32(99)},
				{Topics: []any{"deposit"}, Data: scU32(99)},
				{Topics: []any{"audit"}, Data: scU32(99)},
			},
			opts: []focus.EventOption{focus.IgnoreEventData()},
		},
		{
			name: "the topics can be ignored",
			want: []focus.Event{
				{Topics: []any{"init"}, Data: scU32(1)},
				{Topics: []any{"deposit"}, Data: scU32(2)},
				{Topics: []any{"audit"}, Data: scU32(3)},
			},
			opts: []focus.EventOption{focus.IgnoreEventTopics()},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if !focus.AssertEventsEqual(tb, meta, tc.want, tc.opts...) {
				t.Errorf("the sequence did not match:\n%s", tb.Output())
			}
		})
	}
}

// TestAssertEventsEqualOnAnEmptySequence checks the boundary case: a
// transaction that emitted nothing matches an empty list, and one that
// emitted something does not.
func TestAssertEventsEqualOnAnEmptySequence(t *testing.T) {
	t.Parallel()

	empty := buildSorobanMeta(t)

	tb := faketb.New()
	if !focus.AssertEventsEqual(tb, empty, nil) {
		t.Errorf("an empty sequence did not match:\n%s", tb.Output())
	}

	tb = faketb.New()
	if focus.AssertEventsEqual(tb, empty, []focus.Event{{Topics: []any{"init"}}}) {
		t.Error("a transaction with no events matched a wanted event")
	}
}

// TestAssertEventsEqualFailures is the point of the assertion: the events are
// all there, but the sequence is not the one that was expected, and the
// failure says which event deviated and shows both sides of it.
func TestAssertEventsEqualFailures(t *testing.T) {
	t.Parallel()

	meta := buildSorobanMeta(t,
		eventWithTopics(t, 1, "init"),
		eventWithTopics(t, 2, "deposit"),
		eventWithTopics(t, 3, "audit"),
	)

	tests := []struct {
		name     string
		want     []focus.Event
		opts     []focus.EventOption
		contains []string
	}{
		{
			name: "too many events wanted",
			want: []focus.Event{
				{Topics: []any{"init"}},
				{Topics: []any{"deposit"}},
			},
			contains: []string{"AssertEventsEqual", "meta has 3 event(s), want 2", `"init"`, `"audit"`},
		},
		{
			name: "too few events wanted",
			want: []focus.Event{
				{Topics: []any{"init"}},
				{Topics: []any{"deposit"}},
				{Topics: []any{"audit"}},
				{Topics: []any{"close"}},
			},
			contains: []string{"meta has 3 event(s), want 4"},
		},
		{
			name: "the events are in the wrong order",
			want: []focus.Event{
				{Topics: []any{"init"}},
				{Topics: []any{"audit"}},
				{Topics: []any{"deposit"}},
			},
			contains: []string{
				"event 1 of 3 does not match",
				"actual:", `"deposit"`,
				"expected:", `"audit"`,
			},
		},
		{
			name: "an event is missing",
			want: []focus.Event{
				{Topics: []any{"init"}},
				{Topics: []any{"transfer"}},
				{Topics: []any{"audit"}},
			},
			contains: []string{"event 1 of 3 does not match", `"deposit"`, `"transfer"`},
		},
		{
			name: "the payload differs",
			want: []focus.Event{
				{Topics: []any{"init"}, Data: scU32(1)},
				{Topics: []any{"deposit"}, Data: scU32(99)},
				{Topics: []any{"audit"}, Data: scU32(3)},
			},
			contains: []string{"event 1 of 3 does not match", "u32(2)", "u32(99)"},
		},
		{
			name: "an ignored field is not what failed",
			want: []focus.Event{
				{Topics: []any{"init"}},
				{Topics: []any{"withdraw"}, Data: scU32(99)},
				{Topics: []any{"audit"}},
			},
			opts:     []focus.EventOption{focus.IgnoreEventData()},
			contains: []string{"data ignored by option"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := faketb.New()
			if focus.AssertEventsEqual(tb, meta, tc.want, tc.opts...) {
				t.Fatal("the assertion passed when the sequence did not match")
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

// TestAssertEventsEqualShowsBothSides checks the shape of the diagnostic: the
// event that was emitted and the event that was wanted are printed beside
// each other, so a reader does not have to hold two descriptions in their
// head to see what differs.
func TestAssertEventsEqualShowsBothSides(t *testing.T) {
	t.Parallel()

	meta := buildSorobanMeta(t,
		eventWithTopics(t, 1, "init"),
		eventWithTopics(t, 2, "deposit", "GCJNHFXJEQEY54NPKU4DBMT5WOBJUHMYSFBDAQF4OMTEGJN4BQU7ZST6"),
	)

	tb := faketb.New()
	focus.AssertEventsEqual(tb, meta, []focus.Event{
		{Topics: []any{"init"}},
		{Topics: []any{"withdraw", nil}},
	})
	msg, _ := tb.Message()

	lines := strings.Split(msg, "\n")
	var actual, expected string
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "  actual:"):
			actual = line
		case strings.HasPrefix(line, "  expected:"):
			expected = line
		}
	}
	if actual == "" || expected == "" {
		t.Fatalf("the failure does not show both sides:\n%s", msg)
	}
	if !strings.Contains(actual, `"deposit"`) {
		t.Errorf("the actual event is not shown:\n%s", msg)
	}
	if !strings.Contains(expected, `"withdraw"`) {
		t.Errorf("the wanted event is not shown:\n%s", msg)
	}
	// The wildcard topic is named as such rather than left out.
	if !strings.Contains(expected, "*") {
		t.Errorf("the wildcard topic is not named:\n%s", msg)
	}
	// A field the matcher left open is shown as a wildcard, so the two sides
	// line up and the reader can see the payload was not what failed.
	if !strings.Contains(expected, "data *") {
		t.Errorf("the open payload is not shown as a wildcard:\n%s", msg)
	}
}

func TestAssertSorobanResources(t *testing.T) {
	t.Parallel()

	env := sorobanEnvelope(t, 1000, 200, 300, 100_000_000)

	t.Run("within the budget", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertSorobanResources(tb, env, 1000, 200, 300) {
			t.Errorf("the resources were reported as over budget:\n%s", tb.Output())
		}
	})

	t.Run("a zero budget is not checked", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertSorobanResources(tb, env, 0, 0, 0) {
			t.Errorf("an unchecked dimension was reported:\n%s", tb.Output())
		}
	})

	t.Run("the instructions are over budget", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertSorobanResources(tb, env, 999, 200, 300) {
			t.Fatal("the assertion passed with the instructions over budget")
		}
		msg, _ := tb.Message()
		for _, want := range []string{"AssertSorobanResources", "CPU instructions are 1000, budget is 999"} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})

	t.Run("every dimension over budget is named", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		focus.AssertSorobanResources(tb, env, 1, 1, 1)
		msg, _ := tb.Message()
		for _, want := range []string{
			"CPU instructions are 1000, budget is 1",
			"read bytes are 200, budget is 1",
			"write bytes are 300, budget is 1",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})

	t.Run("the limits are shown", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		focus.AssertSorobanResources(tb, env, 1, 200, 300)
		msg, _ := tb.Message()
		if !strings.Contains(msg, "1000 CPU instructions") {
			t.Errorf("the message does not show the limits the transaction asked for:\n%s", msg)
		}
	})

	t.Run("a transaction with no Soroban data", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertSorobanResources(tb, buildEnvelope(t), 1, 1, 1) {
			t.Fatal("the assertion passed on a transaction with no Soroban data")
		}
		msg, _ := tb.Message()
		if !strings.Contains(msg, "no Soroban data") {
			t.Errorf("the message does not explain the mistake:\n%s", msg)
		}
	})
}

func TestAssertResourceFee(t *testing.T) {
	t.Parallel()

	env := sorobanEnvelope(t, 1000, 200, 300, 100_000_000)

	t.Run("the fee matches", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if !focus.AssertResourceFee(tb, env, 100_000_000) {
			t.Errorf("the resource fee did not match:\n%s", tb.Output())
		}
	})

	t.Run("the fee differs", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertResourceFee(tb, env, 50_000_000) {
			t.Fatal("the assertion passed with the wrong resource fee")
		}
		msg, _ := tb.Message()
		for _, want := range []string{
			"AssertResourceFee",
			"resource fee is 100000000 (10.0000000 XLM)",
			"want 50000000 (5.0000000 XLM)",
		} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})

	t.Run("a transaction with no Soroban data", func(t *testing.T) {
		t.Parallel()

		tb := faketb.New()
		if focus.AssertResourceFee(tb, buildEnvelope(t), 1) {
			t.Fatal("the assertion passed on a transaction with no Soroban data")
		}
		msg, _ := tb.Message()
		if !strings.Contains(msg, "no Soroban data") {
			t.Errorf("the message does not explain the mistake:\n%s", msg)
		}
	})
}
