package focus

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// contractIDString renders a contract id as its strkey "C…" form.
//
// ContractId is a bare 32-byte hash in the generated code, so the strkey
// encoding has to be applied here rather than read off a helper.
func contractIDString(id xdr.ContractId) (string, error) {
	return strkey.Encode(strkey.VersionByteContract, id[:])
}

// Event describes a contract event to look for.
//
// A zero field matches anything, so asserting only that a contract emitted
// something is as easy as asserting on the exact payload. Topics and Data
// accept the same forms as any other value in this package, plus a plain
// string for the common case of a symbol topic:
//
//	focus.AssertEventEmitted(t, meta, focus.Event{
//	    ContractID: "CBGSB…",
//	    Topics:     []any{"transfer", from, to},
//	})
type Event struct {
	// ContractID is the strkey contract address. Empty matches any contract.
	ContractID string
	// Topics are the event topics, in order. Nil matches any topics. A nil
	// element matches any single topic, so []any{"transfer", nil} matches a
	// two-topic event whose first topic is the symbol "transfer".
	Topics []any
	// Data is the event payload. Nil matches any payload.
	Data any
}

// EventOption configures an event assertion.
//
// Options only relax what a matcher checks, never tighten it: an option can
// let a difference through, but it can never turn a match into a failure.
// That keeps a test's meaning simple — the matcher says what must be there,
// and the options say what need not be.
//
// Whatever an option relaxes is named in the failure output, because an
// assertion that quietly stops checking something is how a test starts
// passing for the wrong reason.
type EventOption func(*eventConfig)

// eventConfig holds the resolved settings of an event assertion.
type eventConfig struct {
	ignoreContractID bool
	ignoreTopics     bool
	ignoreData       bool
}

// newEventConfig resolves the options of an event assertion.
func newEventConfig(opts ...EventOption) *eventConfig {
	c := &eventConfig{}
	for _, o := range opts {
		o(c)
	}
	return c
}

// IgnoreEventContractID ignores the contract id of every wanted event, so
// only the payload is compared.
//
// A matcher that names a contract is usually written that way for the
// reader's benefit — the address is the first thing anyone asks about —
// while the test itself is about the topics. This option says so
// explicitly rather than leaving the contract id out of the matcher and
// losing the documentation.
func IgnoreEventContractID() EventOption {
	return func(c *eventConfig) { c.ignoreContractID = true }
}

// IgnoreEventTopics ignores the topics of every wanted event, so only the
// payload and the order of the events are compared.
func IgnoreEventTopics() EventOption {
	return func(c *eventConfig) { c.ignoreTopics = true }
}

// IgnoreEventData ignores the payload of every wanted event, so only the
// topics and the order of the events are compared.
//
// This is the option for a contract whose payload carries a value that
// legitimately differs between runs — a timestamp, a generated id — while
// the sequence of topics is what the test is about.
func IgnoreEventData() EventOption {
	return func(c *eventConfig) { c.ignoreData = true }
}

// ignoredFields names the fields the options relaxed, for the failure output.
func (c *eventConfig) ignoredFields() string {
	var names []string
	if c.ignoreContractID {
		names = append(names, "contract id")
	}
	if c.ignoreTopics {
		names = append(names, "topics")
	}
	if c.ignoreData {
		names = append(names, "data")
	}
	return strings.Join(names, ", ")
}

// AssertEventEmitted checks that a transaction meta contains a matching
// contract event.
func AssertEventEmitted(t TB, meta Value, want Event) bool {
	t.Helper()

	events, ok := contractEvents(t, "AssertEventEmitted", meta)
	if !ok {
		return false
	}

	for _, e := range events {
		if eventMatches(e, want) {
			return true
		}
	}

	t.Errorf("AssertEventEmitted: no event matched %s\n\n%s",
		describeWantedEvent(want), indent(describeEvents(events), "  "))
	return false
}

// AssertEventCount checks how many contract events a meta carries.
func AssertEventCount(t TB, meta Value, want int) bool {
	t.Helper()

	events, ok := contractEvents(t, "AssertEventCount", meta)
	if !ok {
		return false
	}
	if len(events) != want {
		t.Errorf("AssertEventCount: meta has %d event(s), want %d\n\n%s",
			len(events), want, indent(describeEvents(events), "  "))
		return false
	}
	return true
}

// AssertNoEvents checks that a transaction emitted no contract events.
func AssertNoEvents(t TB, meta Value) bool {
	t.Helper()
	return AssertEventCount(t, meta, 0)
}

// AssertEventsEqual checks that the contract events in a meta are exactly the
// wanted events, in the wanted order.
//
// A contract that runs through several steps says so with a sequence of
// events — init, deposit, transfer, audit — and the sequence is the point:
// the same events in a different order describe a different program. This
// assertion pins the whole list, so both the count and the order of the
// events have to be right.
//
// Each wanted event is matched with the same rules as AssertEventEmitted, so
// a zero field matches anything and a nil topic matches any single topic.
// The options relax the comparison for the whole list; see IgnoreEventData
// and its neighbours.
//
// A failure names the index that deviated and prints what was emitted beside
// what was wanted, so the two can be read against each other, and then lists
// every event that was emitted.
func AssertEventsEqual(t TB, meta Value, want []Event, opts ...EventOption) bool {
	t.Helper()
	const assertion = "AssertEventsEqual"

	events, ok := contractEvents(t, assertion, meta)
	if !ok {
		return false
	}

	cfg := newEventConfig(opts...)

	if len(events) != len(want) {
		t.Errorf("%s: meta has %d event(s), want %d\n\n%s",
			assertion, len(events), len(want), indent(describeEvents(events), "  "))
		return false
	}

	for i, w := range want {
		if eventMatchesWith(events[i], w, cfg) {
			continue
		}
		t.Errorf("%s: %s\n\n%s",
			assertion,
			describeEventMismatch(i, len(want), events[i], w, cfg),
			indent(describeEvents(events), "  "))
		return false
	}
	return true
}

// AssertReturnValue checks the value a contract invocation returned.
func AssertReturnValue(t TB, meta Value, want Value) bool {
	t.Helper()

	soroban, ok := sorobanMeta(t, "AssertReturnValue", meta)
	if !ok {
		return false
	}

	wantVal, ok := scVal(want)
	if !ok {
		t.Errorf("AssertReturnValue: want value could not be read as an ScVal")
		return false
	}

	if soroban.ReturnValue.Equals(wantVal) {
		return true
	}

	t.Errorf("AssertReturnValue: contract returned %s, want %s",
		describeScVal(soroban.ReturnValue), describeScVal(wantVal))
	return false
}

// AssertContractTrapped checks that a transaction failed because the contract
// itself trapped, rather than being rejected by the protocol.
//
// This is the Soroban failure developers hit most, and distinguishing it from
// a malformed transaction is usually the first question worth answering.
func AssertContractTrapped(t TB, result Value) bool {
	t.Helper()
	return contractTrapped(t, "AssertContractTrapped", result)
}

// AssertSorobanResources checks that a Soroban transaction's resource limits
// fit inside the given budgets.
//
// The limits are the ones the transaction bids: CPU instructions, bytes read
// from the ledger, and bytes written back to it. They are budgets rather than
// measurements — a transaction asks for what it thinks it needs, and the
// network charges it for that — so this is the assertion a regression suite
// uses to keep a contract's cost from creeping up.
//
// A budget of zero means the dimension is not checked, which is how a test
// pins the instructions without pinning the memory.
//
// A failure names every dimension that went over its budget, and then shows
// the limits the transaction asked for.
func AssertSorobanResources(t TB, envelope Value, maxInstructions uint32, readBytes, writeBytes uint32) bool {
	t.Helper()
	const assertion = "AssertSorobanResources"

	data, ok := sorobanData(t, assertion, envelope)
	if !ok {
		return false
	}

	var over []string
	if maxInstructions > 0 && uint32(data.Resources.Instructions) > maxInstructions {
		over = append(over, fmt.Sprintf("CPU instructions are %d, budget is %d",
			data.Resources.Instructions, maxInstructions))
	}
	if readBytes > 0 && uint32(data.Resources.DiskReadBytes) > readBytes {
		over = append(over, fmt.Sprintf("read bytes are %d, budget is %d",
			data.Resources.DiskReadBytes, readBytes))
	}
	if writeBytes > 0 && uint32(data.Resources.WriteBytes) > writeBytes {
		over = append(over, fmt.Sprintf("write bytes are %d, budget is %d",
			data.Resources.WriteBytes, writeBytes))
	}
	if len(over) == 0 {
		return true
	}

	t.Errorf("%s: %s\n\n%s",
		assertion, strings.Join(over, "; "),
		indent(describeSorobanResources(data.Resources), "  "))
	return false
}

// AssertResourceFee checks the refundable resource fee a Soroban transaction
// bids, in stroops.
//
// The fee is what the sender is willing to pay for the resources the
// transaction reserves; the unused part is refunded once the transaction
// lands. It is a bid rather than a charge, so this is the assertion that
// pins what a contract call costs the caller.
//
// The fee is in XLM, so the failure message shows both units.
func AssertResourceFee(t TB, envelope Value, wantFee int64) bool {
	t.Helper()
	const assertion = "AssertResourceFee"

	data, ok := sorobanData(t, assertion, envelope)
	if !ok {
		return false
	}

	got := int64(data.ResourceFee)
	if got == wantFee {
		return true
	}

	t.Errorf("%s: resource fee is %s, want %s", assertion, stroops(got), stroops(wantFee))
	return false
}

// Events returns the contract events in a meta, as an escape hatch for checks
// this package does not provide. It reports a test failure and returns nil
// when the meta cannot be read.
func Events(t TB, meta Value) []Event {
	t.Helper()

	raw, ok := contractEvents(t, "Events", meta)
	if !ok {
		return nil
	}
	out := make([]Event, 0, len(raw))
	for _, e := range raw {
		out = append(out, toEvent(e))
	}
	return out
}

// sorobanMeta extracts the Soroban section of a transaction meta.
func sorobanMeta(t TB, assertion string, meta Value) (*xdr.SorobanTransactionMeta, bool) {
	t.Helper()

	decoded, ok := resolve(t, assertion, "", meta, "TransactionMeta")
	if !ok {
		return nil, false
	}

	m, ok := decoded.Raw.(*xdr.TransactionMeta)
	if !ok {
		t.Errorf("%s: value is a %s, want a TransactionMeta", assertion, decoded.Type)
		return nil, false
	}

	var soroban *xdr.SorobanTransactionMeta
	if !guard(t, assertion, func() {
		if v3, has := m.GetV3(); has {
			soroban = v3.SorobanMeta
		}
	}) {
		return nil, false
	}
	if soroban == nil {
		t.Errorf("%s: this transaction meta carries no Soroban section, so it has no "+
			"contract events or return value (was the transaction a contract invocation?)", assertion)
		return nil, false
	}
	return soroban, true
}

// contractEvents returns the contract events in a meta.
func contractEvents(t TB, assertion string, meta Value) ([]xdr.ContractEvent, bool) {
	t.Helper()
	soroban, ok := sorobanMeta(t, assertion, meta)
	if !ok {
		return nil, false
	}
	return soroban.Events, true
}

// sorobanData returns the Soroban transaction data of an envelope.
//
// The resource limits and the resource fee live in the transaction's
// extension, so they are only there for a contract invocation; an envelope
// without them is reported rather than treated as one that happens to ask
// for nothing.
func sorobanData(t TB, assertion string, envelope Value) (*xdr.SorobanTransactionData, bool) {
	t.Helper()

	env, _, ok := envelopeFrom(t, assertion, envelope)
	if !ok {
		return nil, false
	}

	var data *xdr.SorobanTransactionData
	if !guard(t, assertion, func() {
		switch {
		case env.V1 != nil:
			data = env.V1.Tx.Ext.SorobanData
		case env.V0 != nil, env.FeeBump != nil:
			// Neither form carries a transaction extension.
			data = nil
		}
	}) {
		return nil, false
	}
	if data == nil {
		t.Errorf("%s: this transaction carries no Soroban data.\n"+
			"Resource limits and the resource fee live in the Soroban transaction data, which only "+
			"a contract invocation has (was this transaction a contract call?)", assertion)
		return nil, false
	}
	return data, true
}

// describeSorobanResources renders the resource limits a transaction bids, so
// a failure shows what was asked for rather than only what was allowed.
func describeSorobanResources(r xdr.SorobanResources) string {
	return fmt.Sprintf("resource limits: %d CPU instructions, %d read bytes, %d write bytes",
		r.Instructions, r.DiskReadBytes, r.WriteBytes)
}

// eventMatches reports whether an event satisfies a matcher.
func eventMatches(e xdr.ContractEvent, want Event) bool {
	return eventMatchesWith(e, want, newEventConfig())
}

// eventMatchesWith reports whether an event satisfies a matcher under the
// given options.
//
// The options only relax the comparison, so the matcher still decides what
// has to be there.
func eventMatchesWith(e xdr.ContractEvent, want Event, cfg *eventConfig) bool {
	if !cfg.ignoreContractID && want.ContractID != "" {
		if e.ContractId == nil {
			return false
		}
		id, err := contractIDString(*e.ContractId)
		if err != nil || id != want.ContractID {
			return false
		}
	}

	body, ok := e.Body.GetV0()
	if !ok {
		return false
	}

	if !cfg.ignoreTopics && want.Topics != nil {
		if len(want.Topics) != len(body.Topics) {
			return false
		}
		for i, wt := range want.Topics {
			if wt == nil {
				continue
			}
			wv, ok := scVal(wt)
			if !ok || !body.Topics[i].Equals(wv) {
				return false
			}
		}
	}

	if !cfg.ignoreData && want.Data != nil {
		wv, ok := scVal(want.Data)
		if !ok || !body.Data.Equals(wv) {
			return false
		}
	}
	return true
}

// scVal converts an accepted value into an ScVal.
//
// A plain string is treated as a symbol, because a symbol topic is by far the
// most common thing a test writes, and requiring the base64 of
// ScVal{Sym:"transfer"} would make the common case unreadable.
func scVal(v Value) (xdr.ScVal, bool) {
	switch val := v.(type) {
	case xdr.ScVal:
		return val, true
	case *xdr.ScVal:
		if val == nil {
			return xdr.ScVal{}, false
		}
		return *val, true
	case string:
		// Try base64 XDR first; fall back to treating it as a symbol.
		decoded, err := coerce(val, "ScVal")
		if err == nil {
			if sv, ok := decoded.Raw.(*xdr.ScVal); ok {
				return *sv, true
			}
		}
		sym := xdr.ScSymbol(val)
		return xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym}, true
	default:
		decoded, err := coerce(v, "ScVal")
		if err != nil {
			return xdr.ScVal{}, false
		}
		sv, ok := decoded.Raw.(*xdr.ScVal)
		if !ok {
			return xdr.ScVal{}, false
		}
		return *sv, true
	}
}

// toEvent converts an XDR event into the matcher-shaped form returned by
// Events.
func toEvent(e xdr.ContractEvent) Event {
	out := Event{}
	if e.ContractId != nil {
		if id, err := contractIDString(*e.ContractId); err == nil {
			out.ContractID = id
		}
	}
	if body, ok := e.Body.GetV0(); ok {
		for _, tpc := range body.Topics {
			out.Topics = append(out.Topics, tpc)
		}
		out.Data = body.Data
	}
	return out
}

// describeEvents renders the events actually present, so a non-match shows
// what was there instead of only what was missing.
func describeEvents(events []xdr.ContractEvent) string {
	if len(events) == 0 {
		return "the transaction emitted no contract events"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "events present (%d):\n", len(events))
	for i, e := range events {
		contract := "(no contract)"
		if e.ContractId != nil {
			if id, err := contractIDString(*e.ContractId); err == nil {
				contract = shorten(id)
			}
		}
		fmt.Fprintf(&b, "  [%d] %s", i, contract)

		if body, ok := e.Body.GetV0(); ok {
			fmt.Fprintf(&b, "  %s", describeEventFields(body))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// describeEventFields renders an event's topics and payload on one line, so
// an event can be shown beside the event that was wanted.
func describeEventFields(body xdr.ContractEventV0) string {
	topics := make([]string, 0, len(body.Topics))
	for _, tpc := range body.Topics {
		topics = append(topics, describeScVal(tpc))
	}
	return fmt.Sprintf("topics[%s]  data %s", strings.Join(topics, ", "), describeScVal(body.Data))
}

// describeEventMismatch renders the event that deviated, with what was
// emitted beside what was wanted.
//
// The two are printed on adjacent lines so they can be read against each
// other: a failure names the index, and the reader should not have to hold
// two descriptions in their head to see which topic or payload differs.
func describeEventMismatch(index, total int, actual xdr.ContractEvent, want Event, cfg *eventConfig) string {
	var b strings.Builder
	fmt.Fprintf(&b, "event %d of %d does not match\n", index, total)

	actualFields := "(the event body carries no V0 arm, so it has no topics or data)"
	body, hasBody := actual.Body.GetV0()
	if hasBody {
		actualFields = describeEventFields(body)
	}

	fmt.Fprintf(&b, "  actual:   %s\n", actualFields)
	fmt.Fprintf(&b, "  expected: %s\n", describeWantedFieldsWith(want, cfg, hasBody))

	if ignored := cfg.ignoredFields(); ignored != "" {
		fmt.Fprintf(&b, "  (%s ignored by option, so it did not have to match)\n", ignored)
	}
	return strings.TrimRight(b.String(), "\n")
}

// describeWantedEvent renders a matcher, naming which fields were wildcards
// so a failing match is diagnosable.
func describeWantedEvent(want Event) string {
	return describeWantedFields(want, newEventConfig())
}

// describeWantedFields renders the fields a matcher checks, naming the
// wildcards so a failing match is diagnosable.
//
// A field the options relaxed is left out, because it is not part of what
// the matcher is checking any more.
func describeWantedFields(want Event, cfg *eventConfig) string {
	return describeWantedFieldsWith(want, cfg, false)
}

// describeWantedFieldsWith renders the fields a matcher checks, alongside the
// event that was emitted.
//
// A field the matcher left open is shown as a wildcard when the emitted event
// has one, so the two sides of the comparison line up and a reader can see
// at a glance that the payload was not what failed.
func describeWantedFieldsWith(want Event, cfg *eventConfig, openBody bool) string {
	var parts []string
	if !cfg.ignoreContractID && want.ContractID != "" {
		parts = append(parts, "contract "+shorten(want.ContractID))
	}
	if !cfg.ignoreTopics {
		switch {
		case want.Topics != nil:
			topics := make([]string, 0, len(want.Topics))
			for _, tpc := range want.Topics {
				if tpc == nil {
					topics = append(topics, "*")
					continue
				}
				if sv, ok := scVal(tpc); ok {
					topics = append(topics, describeScVal(sv))
					continue
				}
				topics = append(topics, fmt.Sprintf("%v", tpc))
			}
			parts = append(parts, "topics["+strings.Join(topics, ", ")+"]")
		case openBody:
			parts = append(parts, "topics[*]")
		}
	}
	if !cfg.ignoreData {
		switch {
		case want.Data != nil:
			if sv, ok := scVal(want.Data); ok {
				parts = append(parts, "data "+describeScVal(sv))
			}
		case openBody:
			parts = append(parts, "data *")
		}
	}
	if len(parts) == 0 {
		return "any event"
	}
	return strings.Join(parts, "  ")
}

// describeScVal renders an ScVal compactly for a one-line event listing.
func describeScVal(v xdr.ScVal) string {
	switch v.Type {
	case xdr.ScValTypeScvSymbol:
		if s, ok := v.GetSym(); ok {
			return fmt.Sprintf("%q", string(s))
		}
	case xdr.ScValTypeScvString:
		if s, ok := v.GetStr(); ok {
			return fmt.Sprintf("%q", string(s))
		}
	case xdr.ScValTypeScvU32:
		if n, ok := v.GetU32(); ok {
			return fmt.Sprintf("u32(%d)", n)
		}
	case xdr.ScValTypeScvI32:
		if n, ok := v.GetI32(); ok {
			return fmt.Sprintf("i32(%d)", n)
		}
	case xdr.ScValTypeScvU64:
		if n, ok := v.GetU64(); ok {
			return fmt.Sprintf("u64(%d)", n)
		}
	case xdr.ScValTypeScvI64:
		if n, ok := v.GetI64(); ok {
			return fmt.Sprintf("i64(%d)", n)
		}
	case xdr.ScValTypeScvBool:
		if b, ok := v.GetB(); ok {
			return fmt.Sprintf("%t", b)
		}
	case xdr.ScValTypeScvVoid:
		return "void"
	case xdr.ScValTypeScvAddress:
		if a, ok := v.GetAddress(); ok {
			if s, err := a.String(); err == nil {
				return shorten(s)
			}
		}
	case xdr.ScValTypeScvU128:
		if n, ok := v.GetU128(); ok {
			return fmt.Sprintf("u128(%s)", u128String(n))
		}
	case xdr.ScValTypeScvI128:
		if n, ok := v.GetI128(); ok {
			return fmt.Sprintf("i128(%s)", i128String(n))
		}
	case xdr.ScValTypeScvBytes:
		if b, ok := v.GetBytes(); ok {
			return describeBytes(b)
		}
	case xdr.ScValTypeScvVec:
		if vec, ok := v.GetVec(); ok {
			return describeScVec(vec)
		}
	case xdr.ScValTypeScvMap:
		if m, ok := v.GetMap(); ok {
			return describeScMap(m)
		}
	case xdr.ScValTypeScvContractInstance:
		return "contract instance"
	case xdr.ScValTypeScvLedgerKeyContractInstance:
		return "contract instance key"
	case xdr.ScValTypeScvLedgerKeyNonce:
		return "nonce key"
	case xdr.ScValTypeScvError:
		if e, ok := v.GetError(); ok {
			return describeScError(e)
		}
	case xdr.ScValTypeScvTimepoint:
		if t, ok := v.GetTimepoint(); ok {
			return fmt.Sprintf("timepoint(%d)", t)
		}
	case xdr.ScValTypeScvDuration:
		if d, ok := v.GetDuration(); ok {
			return fmt.Sprintf("duration(%d)", d)
		}
	}
	return strings.TrimPrefix(v.Type.String(), "ScValTypeScv")
}

// describeScVec renders a vector compactly, truncating long ones so a large
// payload does not dominate a failure message.
func describeScVec(vec *xdr.ScVec) string {
	if vec == nil || len(*vec) == 0 {
		return "vec[]"
	}
	const maxShown = 3
	parts := make([]string, 0, maxShown+1)
	for i, element := range *vec {
		if i == maxShown {
			parts = append(parts, fmt.Sprintf("… +%d", len(*vec)-maxShown))
			break
		}
		parts = append(parts, describeScVal(element))
	}
	return "vec[" + strings.Join(parts, ", ") + "]"
}

// describeScMap renders a map compactly, truncating long ones.
func describeScMap(m *xdr.ScMap) string {
	if m == nil || len(*m) == 0 {
		return "map{}"
	}
	const maxShown = 3
	parts := make([]string, 0, maxShown+1)
	for i, entry := range *m {
		if i == maxShown {
			parts = append(parts, fmt.Sprintf("… +%d", len(*m)-maxShown))
			break
		}
		parts = append(parts, fmt.Sprintf("%s: %s",
			describeScVal(entry.Key), describeScVal(entry.Val)))
	}
	return "map{" + strings.Join(parts, ", ") + "}"
}

// describeBytes renders a byte array as hex, truncated so a large payload does
// not dominate a failure message.
func describeBytes(b xdr.ScBytes) string {
	const maxShown = 8
	if len(b) == 0 {
		return "bytes(0x)"
	}
	if len(b) > maxShown {
		return fmt.Sprintf("bytes(0x%x…, %d bytes)", b[:maxShown], len(b))
	}
	return fmt.Sprintf("bytes(0x%x)", b)
}

// u128String renders the two-word unsigned representation as one decimal.
func u128String(parts xdr.UInt128Parts) string {
	value := new(big.Int).SetUint64(uint64(parts.Hi))
	value.Lsh(value, 64)
	value.Or(value, new(big.Int).SetUint64(uint64(parts.Lo)))
	return value.String()
}

// i128String renders the two-word signed representation as one decimal. Lsh on
// the signed high word sign-extends, so adding the low word gives the value.
func i128String(parts xdr.Int128Parts) string {
	value := new(big.Int).SetInt64(int64(parts.Hi))
	value.Lsh(value, 64)
	value.Add(value, new(big.Int).SetUint64(uint64(parts.Lo)))
	return value.String()
}

// shorten abbreviates a strkey for inline display, matching how lens and
// prism abbreviate addresses.
func shorten(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:5] + "…" + s[len(s)-4:]
}
