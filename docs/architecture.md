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
cmd/focus/            main; maps errors to exit codes
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
  simulation.go       simulated resource budget assertions
  fees.go             consumed and refundable fee assertions
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
  (`TestAuditPassesOnRepo`).
