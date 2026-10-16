# Architecture

How stellar-focus fits together, and where to make the changes contributors
most often want to make:

- [Adding an assertion](#adding-an-assertion)
- [Adding a fixture source](#adding-a-fixture-source)

## The organising principle

**The failure message is the product.**

Everything else — the input coercion, the ignore rules, the golden format — is
in service of one thing: when a test goes red, the developer should know what
changed without leaving the terminal. A general-purpose assertion library
already compares two strings correctly. The only reason to adopt this one is
that it understands XDR, and it demonstrates that in the failure output.

So the design questions that get the most weight here are *what does this
print when it fails*, not *what does this return when it passes*.

## Package layout

```
cmd/focus/            main; maps errors to exit codes, registers `focus assert`
  interactive.go      `focus assert`: interactive assertion generator
internal/cli/         cobra commands: capture, verify
internal/faketb/      recording focus.TB — lets focus test its own messages
internal/fakesource/  in-memory capture.Source, so the CLI tests run offline
pkg/focus/
  focus.go            TB, Value coercion, panic guard, Encode
  safe_exec.go        panic-safe harness: SafeExec, SafeCall, PanicError
  terminal.go         ANSI colour helpers, field highlighting, TTY detection
  options.go          ignore and normalize options
  ignore_rules.go     ignore path matching: wildcards, globs, case-insensitive
  message.go          failure-message rendering        ← the product
  equal.go            structural equality assertions   ← seam: new assertions
  fields.go           envelope field assertions
  timebounds.go       time and ledger bound assertions
  memo.go             memo value assertions
  op_order.go         operation sequence assertions
  signatures.go       signer presence and threshold assertions
  results.go          transaction result-code assertions
  result_codes.go     per-operation result-code assertions
  soroban.go          events, return values
  diagnostic_order.go diagnostic stream order and timelines
  simulation.go       simulated resource budget assertions
  fees.go             consumed and refundable fee assertions
  adapters.go         testify/assert and gomock-style adapters around assertions
  require/            Fatalf mirrors of everything above
  require/audit.go    AST parity audit between focus and require
pkg/golden/           golden files, -focus.update
pkg/capture/          Source interface, RPC impl, Verify  ← seam: new sources
```

### Why the CLI is not in pkg/

`pkg/focus` is imported into other people's test suites. A test helper that
drags a CLI framework into a production module's dependency graph is a helper
people resent, so Cobra lives only in `cmd/` and `internal/cli`, which the
library never imports.

The permitted dependency set is: the standard library, `stellar/go-stellar-sdk`,
and `stellar-xdr-lens`. That is enforced by `TestLibraryStaysDependencyLight`
(compiled graph via `go list -deps`) and by the AST check in
`pkg/focus/deps_lint_test.go` (import specs, pinpointing file and line)
rather than left to discipline.

## The TB seam

```go
type TB interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Name() string
}
```

Taking an interface rather than `*testing.T` buys three things:

1. Assertions work in `testing.B` and `testing.F`.
2. A project can wrap them in its own helpers.
3. **focus can test its own failure messages**, by passing
   `internal/faketb.TB`, which records instead of failing. Since the output is
   the contract, this is not a convenience — it is what makes the contract
   testable at all.

`Helper()` is called by every assertion so `go test` reports the caller's
file and line rather than a line inside this library.

## Value coercion

Assertions take `Value` (an alias for `any`) and accept a base64 string, raw
`[]byte`, any `xdr.*` value implementing `encoding.BinaryMarshaler`, or a
`*lens.Value`.

This trades static type safety for ergonomics, deliberately. Test code holds
XDR in whichever form the system under test produced, and requiring a
conversion at every call site is friction in exactly the common case. An
unrecognised type fails immediately with a message naming what *is* accepted,
so the mistake surfaces as an input error rather than a confusing diff.

Coercion happens in one place, `coerce`, so every assertion produces the same
shape of message when an input is wrong.

## Never panic

Generated XDR accessors dereference union arms without checking the
discriminant, so a malformed value can panic deep inside the SDK. A panic
escaping into someone's test binary destroys the output of every other test in
the package.

Every assertion runs its inspection behind `guard`, which converts a panic
into an ordinary test failure that also asks for a bug report.
`pkg/focus/safe_exec.go` generalises this as `SafeExec`/`SafeCall` with a
`PanicError` that unwraps to the original cause for `errors.Is`/`errors.As`.
`TestAssertionsNeverPanic` feeds nil, empty strings, non-base64, truncated
payloads, wrong types and mismatched XDR through every exported assertion.

## Terminal output

Colour highlights exactly which field mismatched. `pkg/focus/terminal.go`
provides the ANSI helpers (`HighlightPath`, `FormatFieldMismatch`,
`StripANSI`/`ContainsANSI`) and TTY detection (`SupportsColorWriter`,
`PaletteForWriter`, `ColorEnabled`), matching lens's `ColorPalette` so a
focus failure and `lens diff` highlight the same way. Test output is
usually captured, so colour defaults to off via `focus.WithColor("never")`
and is enabled explicitly with `focus.WithColor("always")`; `auto` honours
`NO_COLOR` and `TERM=dumb`.

## Ignore rules

Without them the library is unusable on real suites — a rebuilt transaction
differs in sequence number, signatures and time bounds every run.

Patterns match `lens.Change.Path`, the dotted path lens already reports
(`V1.Tx.Operations[0].Body.PaymentOp.Amount`). Because lens does the
traversal, focus only has to *filter a list*, which keeps `ignore_rules.go`
small. The pattern language is deliberately forgiving, since an ignore
pattern that does not match is worse than useless:

- `*` matches one segment, `**` matches any number of segments, and an index
  can be wildcarded (`Operations[*]`);
- within a segment, `*`, `?` and character classes (`[abc]`, `[!abc]`) work
  like globs;
- matching is case-insensitive, so patterns can be written the way the field
  reads (`operations[*].sourceaccount`);
- a pattern is anchored to the tail of a path as well as its whole, so
  `operations[*].sourceaccount` does not need the `V1.Tx.` prefix.

Two rules that exist for trust rather than function:

- **Applied ignores are named in the failure output.** A difference that
  vanished without explanation makes every future failure less believable.
- **An ignore that matches nothing fails the assertion**, even on an otherwise
  passing comparison. A stale pattern is a test silently weakening; the moment
  it stops matching is the only moment anyone would notice.

## Diagnostic event order and timelines

A Soroban transaction's meta carries a *diagnostic* stream alongside the events
the contract chose to emit: the frames the host opened and closed (`fn_call`,
`fn_return`), the lines the contract logged, and the events of calls that
failed — which survive nowhere else. `diagnostic_order.go` asserts on that
stream, and on the two things about it that can be checked: the order of the
events, and the ledger the stream belongs to.

**Order is the assertion.** The stream is the host's account of what the call
did as it did it, so the right events in the wrong order describe a different
program. `AssertDiagnosticEventOrder` matches the wanted events as a
subsequence: each is looked for at or after the position the previous one was
found at, so listing every event pins every position and listing two pins only
their relative order. A failure names the wanted event that went unmatched,
where the search had reached, and — the case worth catching — that the same
event is present earlier in the stream, which is what a swap looks like.

**A diagnostic event has no timestamp.** Its XDR is a bool and a contract
event; there is no wall clock in it. The protocol's only time coordinate is the
ledger the transaction was applied in, so that is what
`AssertDiagnosticEventTimeline` checks, together with the close time that gives
the ledger its place in the chain. Both are *passed in* rather than looked up:
reading a clock or asking a node would make the assertion neither deterministic
nor offline, and `focus capture` already records exactly these two values as
fixture provenance (`golden.Provenance.Captured`).

The ledger is read from the meta rather than taken on trust. A transaction
writes the entries it changes during the ledger it executes in, so their
`LastModifiedLedgerSeq` is that ledger — and the *newest* pin is the one that
answers the question, because an entry restored out of the Soroban archival
state can keep an older last-modified ledger. A meta that pins no ledger, like
an empty stream, is reported as unplaceable rather than quietly passing a check
it was never able to make.

Reading the stream is one seam, `diagnosticStream`: it resolves the value
through the same `coerce` every assertion uses, walks it with the SDK's
`GetDiagnosticEvents` behind `guard`, and decorates each event with its index,
call depth and a pre-rendered description. Pre-rendering is deliberate — the
description is built while the value is known to be well-formed, so printing a
failure never calls a generated accessor on a value that could panic.

## Golden format

```json
{ "format", "type", "description", "xdr", "captured", "decoded" }
```

Field order is the file order, chosen so a diff shows identity first and the
large decoded tree last.

**`xdr` is the source of truth; `decoded` is for humans.** Comparisons never
read `decoded` — doing so would make assertions depend on lens's formatting,
so a cosmetic change in lens would fail everyone's tests. `decoded` exists
purely so a pull-request diff is reviewable, which is the whole reason not to
store a bare blob.

`format` is checked on load, so a file written by a future version is rejected
rather than silently misread.

## Capture and verify

`capture.Source` is the only thing in the repository that opens a network
connection:

```go
type Source interface {
	Name() string
	Network() string
	Endpoint() string
	Transaction(ctx context.Context, hash string) ([]Record, error)
	LedgerEntry(ctx context.Context, key string) ([]Record, error)
}
```

Everything above it is offline by construction, which is why `go test ./...`
needs no network — including the tests of the capture and verify commands,
which run against `internal/fakesource`.

`Capture` takes the timestamp as a parameter rather than reading the clock, so
fixtures are byte-stable and a test can assert on the whole file.

`Verify` refuses fixtures with no provenance instead of passing them. A
hand-written fixture is not stale; it is simply not checkable this way, and
saying so is more useful than a green tick that means nothing.

## Adding an assertion

1. Put it in the file matching its subject — `fields.go`, `results.go`,
   `soroban.go` — or start a new one.
2. Take `TB` first, return `bool`, call `t.Helper()`.
3. Resolve inputs through `resolve` or `summarize` so bad input produces the
   standard message.
4. Wrap anything that touches generated XDR accessors in `guard` (or
   `SafeExec`/`SafeCall` for standalone inspection).
5. Build the failure message from `message.go` helpers, and **write a test
   that asserts on the message text**, not merely that it failed.
6. Add the mirror in `require/` and keep `require.Audit` green: every
   `Assert* bool` needs a same-signature `Fatalf` counterpart
   (`TestAuditPassesOnRepo` enforces this).

Prefer building on `lens.Explain` over reaching into XDR directly: it means
the value a test asserts on is the same value `lens explain` prints, so an
assertion and a manual investigation can never disagree.

## Assertion adapters

`pkg/focus/adapters.go` lets focus assertions be reported by ecosystems that
do not take a `TB`, without any of them entering this repository's
dependency graph — the compiled-graph and AST tests fail the build if
testify or gomock appears in `pkg/focus`'s imports, test files included.

Two shapes are provided:

- **Errors.** `focus.Check` runs any assertion against a private recording
  TB and returns its failure text as an error; `ErrSucceeded`, `ErrResultCode`
  and the rest of the `results.go` set are shorthand around the corresponding
  assertions. testify's `assert.NoError(t, err)` and gocheck's
  `assert(c, IsNil, err)` then print the same descriptive diff a direct focus
  failure would have printed.
- **Matchers.** `focus.Matcher` declares exactly the method set both gomock
  modules require — `Matches(x any) error` plus `String() string` — and
  `Match` plus the built-ins (`MatchSequence`, `MatchSourceAccount`,
  `MatchResultCode`, …) build values that satisfy it structurally. A
  generated mock accepts them as argument expectations; a miss returns the
  underlying assertion's own failure text, and `String()` is what gomock
  prints for the expectation.

Adapters obey the same rules as assertions: never panicking (a panic inside
the assertion is recovered by `SafeCall` and returned as an error carrying
the bug-report prompt), no I/O, no network. `adapters_test.go` proves each
adapter's error is byte-identical to what the direct assertion reports to
`internal/faketb`.

## The `focus assert` command

`focus assert <tx-xdr>` is a terminal assistant that turns a transaction
someone already has into the assertions describing it: it decodes the base64
with lens, offers one suggestion per field the transaction carries, and
prints the kept ones as paste-ready Go (`cmd/focus/interactive.go`).

What it promises:

- **stdout is generated code, stderr is the conversation.** Prompts,
  suggestions and counts go to stderr, so `focus assert … > snippet.go`
  yields nothing but calls, and `--all` emits everything with no prompting.
- **Suggestions are honest.** Each is built from the same `lens.Explain`
  summary the assertions compare against, optional fields (memo, fee-bump
  wrapper, time and ledger bounds) are offered only when the transaction
  carries them, and `--var` names the generated variable. Every suggestion
  keeps the assertion behind it, so the tests can run each one against its
  own transaction (passes) and against a different one (fails with a
  descriptive diff) through `internal/faketb`.
- **End of input keeps the rest.** Answers are `y` or Enter, `n`, `a`, `q`;
  when stdin ends, the remaining suggestions are kept, so a short script
  never truncates the output.
- **Offline.** Decode, summary and every suggested assertion run locally;
  there is no network call on this path.

The command is assembled in `cmd/focus` and registered with the root
command through `cli.Execute`'s extra-command parameter, which keeps the
interactive flow beside `main` while `internal/cli` stays about fixtures.

## Adding a fixture source

Implement `capture.Source` and wire it into `internal/cli`. Horizon and a
local core instance are the obvious candidates. Return one `Record` per
payload — an envelope, a result and a meta are three fixtures, not one — and
populate `Ledger` and `TxHash` so `focus verify` can re-fetch later.

Nothing in `pkg/golden` or `pkg/focus` needs to change.

## Testing approach

Fixtures in `testdata/` are real captured mainnet transactions, shared with
the sibling repositories so all three exercise the same data.

The suite targets properties that would make the library untrustworthy if
violated:

- failure messages name the changed path, show enriched values, and disclose
  ignored paths (`TestAssertEnvelopeEqualFailureMessage`);
- ignore patterns accept globs, case-insensitive names and tail-anchored
  paths (`TestIgnorePathsGlobAndTailMatching`);
- a stale ignore pattern is reported (`TestStaleIgnorePatternIsReported`);
- no assertion panics on hostile input (`TestAssertionsNeverPanic`);
- golden files are line-oriented and carry real field names
  (`TestGoldenFileIsReviewable`);
- captures are deterministic (`TestCaptureIsDeterministic`);
- the library stays dependency-light (`TestLibraryStaysDependencyLight` and
  the AST check `TestLibraryImportsStayLightAST`, which also bans heavy
  assertion/CLI frameworks);
- colourised failures highlight the mismatched field and strip cleanly to the
  plain message (`TestTerminalAssertionOutputViaFakeTB`);
- panics become ordinary failures with a bug-report prompt (`TestSafeExec*`);
- every `Assert*` has a `Fatalf` mirror with a matching signature
  (`TestAuditPassesOnRepo`);
- diagnostic streams are matched in order, with an absence and a swap named
  differently (`TestAssertDiagnosticEventOrderMessages`);
- an adapter's error is byte-identical to the failure the direct assertion
  reports to `internal/faketb`, and adapters never panic on hostile input
  (`TestErrAdaptersAgreeWithTheDirectAssertions`, `TestMatchersNeverPanic`);
- assertions generated by `focus assert` hold against their own transaction
  and fail with a descriptive diff against a different one
  (`TestGeneratedAssertionsHoldAndFailDescriptively`).
- a diagnostic stream is pinned to the ledger its meta pins, and a stream that
  cannot be placed is reported rather than passed
  (`TestAssertDiagnosticEventTimelineMessages`).
