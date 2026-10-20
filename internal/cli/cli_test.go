package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stellar-optics/stellar-focus/internal/fakesource"
	"github.com/stellar-optics/stellar-focus/pkg/capture"
	"github.com/stellar-optics/stellar-focus/pkg/golden"
)

const testHash = "2b2e276a8f99a2927580eca2c8d665c0adaef9526476f5549269798380a66648"

// withFake swaps the capture source and the clock so the commands run offline
// and produce byte-stable output.
func withFake(t *testing.T, src *fakesource.Source) {
	t.Helper()

	originalSource, originalNow := newSource, now
	newSource = func(string, string, time.Duration) capture.Source { return src }
	now = func() time.Time { return time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { newSource, now = originalSource, originalNow })
}

func run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	err = Execute(args, &out, &errBuf)
	return out.String(), errBuf.String(), err
}

func fixtureXDR(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	return strings.TrimSpace(string(b))
}

func sourceWithTx(t *testing.T) *fakesource.Source {
	t.Helper()
	src := fakesource.New(capture.NetworkTestnet, "https://fake.example")
	src.Transactions[testHash] = []capture.Record{
		{Name: "envelope", Type: "TransactionEnvelope", XDR: fixtureXDR(t, "tx_failed.env.txt"),
			Ledger: 1234, TxHash: testHash},
		{Name: "result", Type: "TransactionResult", XDR: fixtureXDR(t, "tx_failed.res.txt"),
			Ledger: 1234, TxHash: testHash},
	}
	return src
}

func TestCaptureTxWritesFixtures(t *testing.T) {
	withFake(t, sourceWithTx(t))
	dir := t.TempDir()

	stdout, _, err := run(t, "capture", "tx", testHash, "--out", dir)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	for _, name := range []string{"envelope.golden.json", "result.golden.json"} {
		path := filepath.Join(dir, name)
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("%s was not written: %v", path, statErr)
		}
		f, loadErr := golden.Load(path)
		if loadErr != nil {
			t.Fatalf("loading %s: %v", path, loadErr)
		}
		if f.Captured == nil || f.Captured.TxHash != testHash {
			t.Errorf("%s has no usable provenance", name)
		}
	}
	if !strings.Contains(stdout, "wrote") {
		t.Errorf("capture printed nothing useful:\n%s", stdout)
	}
}

func TestCaptureRespectsPrefix(t *testing.T) {
	withFake(t, sourceWithTx(t))
	dir := t.TempDir()

	if _, _, err := run(t, "capture", "tx", testHash, "--out", dir, "--prefix", "failed_swap"); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "failed_swap_envelope.golden.json")); err != nil {
		t.Errorf("prefixed fixture was not written: %v", err)
	}
}

func TestCaptureMasksAddressesWhenRequested(t *testing.T) {
	withFake(t, sourceWithTx(t))
	dir := t.TempDir()
	if _, _, err := run(t, "capture", "tx", testHash, "--out", dir, "--mask-addresses"); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	fixture, err := golden.Load(filepath.Join(dir, "envelope.golden.json"))
	if err != nil {
		t.Fatalf("loading masked fixture: %v", err)
	}
	if fixture.Captured == nil || !fixture.Captured.Anonymized {
		t.Fatal("masked fixture provenance does not record anonymization")
	}
	if fixture.XDR == fixtureXDR(t, "tx_failed.env.txt") {
		t.Fatal("--mask-addresses did not change the captured XDR")
	}
}

// TestCaptureRefusesToClobber protects a hand-curated fixture from being
// silently replaced.
func TestCaptureRefusesToClobber(t *testing.T) {
	withFake(t, sourceWithTx(t))
	dir := t.TempDir()

	if _, _, err := run(t, "capture", "tx", testHash, "--out", dir); err != nil {
		t.Fatalf("first capture failed: %v", err)
	}

	_, _, err := run(t, "capture", "tx", testHash, "--out", dir)
	if err == nil {
		t.Fatal("second capture overwrote the fixture without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error does not mention how to proceed: %v", err)
	}

	if _, _, err := run(t, "capture", "tx", testHash, "--out", dir, "--force"); err != nil {
		t.Errorf("--force did not permit the overwrite: %v", err)
	}
}

func TestCaptureReportsAMissingTransaction(t *testing.T) {
	withFake(t, fakesource.New(capture.NetworkTestnet, "https://fake.example"))

	_, _, err := run(t, "capture", "tx", "nope", "--out", t.TempDir())
	if err == nil {
		t.Fatal("Execute() error = nil for a transaction that does not exist")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error does not say the transaction was not found: %v", err)
	}
}

func TestVerifyReportsMatchingFixtures(t *testing.T) {
	src := sourceWithTx(t)
	withFake(t, src)
	dir := t.TempDir()

	if _, _, err := run(t, "capture", "tx", testHash, "--out", dir); err != nil {
		t.Fatalf("capture failed: %v", err)
	}

	stdout, _, err := run(t, "verify", dir)
	if err != nil {
		t.Fatalf("verify reported drift for unchanged fixtures: %v", err)
	}
	if !strings.Contains(stdout, "ok") || !strings.Contains(stdout, "2 checked") {
		t.Errorf("verify output is not a usable summary:\n%s", stdout)
	}
}

// TestVerifyDetectsDrift is the reason the command exists.
func TestVerifyDetectsDrift(t *testing.T) {
	src := sourceWithTx(t)
	withFake(t, src)
	dir := t.TempDir()

	if _, _, err := run(t, "capture", "tx", testHash, "--out", dir); err != nil {
		t.Fatalf("capture failed: %v", err)
	}

	// The chain now returns something else for the same hash.
	src.Transactions[testHash] = []capture.Record{
		{Name: "envelope", Type: "TransactionEnvelope", XDR: fixtureXDR(t, "tx_success.env.txt"),
			Ledger: 1234, TxHash: testHash},
		{Name: "result", Type: "TransactionResult", XDR: fixtureXDR(t, "tx_failed.res.txt"),
			Ledger: 1234, TxHash: testHash},
	}

	_, stderr, err := run(t, "verify", dir)
	if err == nil {
		t.Fatal("verify succeeded despite drift")
	}
	if code, _ := ExitCode(err); code == 0 {
		t.Error("verify exited 0 despite drift")
	}
	if !strings.Contains(stderr, "DRIFTED") {
		t.Errorf("verify did not flag the drifted fixture:\n%s", stderr)
	}
	// The drift must be explained, not merely announced.
	if !strings.Contains(stderr, "~") && !strings.Contains(stderr, "SeqNum") {
		t.Errorf("verify did not show what changed:\n%s", stderr)
	}
}

// TestVerifySkipsHandWrittenFixtures checks that an unverifiable fixture is
// reported as skipped rather than quietly passing.
func TestVerifySkipsHandWrittenFixtures(t *testing.T) {
	src := fakesource.New(capture.NetworkTestnet, "https://fake.example")
	withFake(t, src)

	dir := t.TempDir()
	f, err := golden.Build(fixtureXDR(t, "tx_failed.env.txt"), "TransactionEnvelope", "hand written", nil)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if err := golden.Save(filepath.Join(dir, "hand.golden.json"), f); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	stdout, _, err := run(t, "verify", dir)
	if err != nil {
		t.Fatalf("verify failed on a hand-written fixture: %v", err)
	}
	if !strings.Contains(stdout, "SKIP") {
		t.Errorf("hand-written fixture was not reported as skipped:\n%s", stdout)
	}
	if src.Calls != 0 {
		t.Errorf("the network was contacted %d time(s) for an unverifiable fixture", src.Calls)
	}
}

func TestVerifyReportsAnEmptyDirectory(t *testing.T) {
	withFake(t, fakesource.New(capture.NetworkTestnet, "https://fake.example"))

	_, _, err := run(t, "verify", t.TempDir())
	if err == nil {
		t.Fatal("verify succeeded with no fixtures to check")
	}
	if !strings.Contains(err.Error(), "no fixture files") {
		t.Errorf("error is not specific: %v", err)
	}
}

func TestUnknownNetworkIsRejectedBeforeAnyRequest(t *testing.T) {
	src := fakesource.New(capture.NetworkTestnet, "https://fake.example")
	withFake(t, src)

	_, _, err := run(t, "capture", "tx", testHash, "--network", "wonderland", "--out", t.TempDir())
	if err == nil {
		t.Fatal("an unknown network was accepted")
	}
	if src.Calls != 0 {
		t.Error("a request was made despite the unknown network")
	}
}

// TestRefreshRewritesOnlyTheDecodedSection is the promise the command makes:
// the XDR is the source of truth, so a refresh may not touch it.
func TestRefreshRewritesOnlyTheDecodedSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "envelope.golden.json")

	fixture := staleFixture(t)
	if err := golden.Save(path, fixture); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	stdout, _, err := run(t, "refresh", dir)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if !strings.Contains(stdout, "updated") {
		t.Errorf("refresh did not report the update:\n%s", stdout)
	}

	refreshed, err := golden.Load(path)
	if err != nil {
		t.Fatalf("loading the refreshed fixture: %v", err)
	}
	if refreshed.XDR != fixture.XDR {
		t.Error("the refresh changed the XDR")
	}
	if refreshed.Type != fixture.Type {
		t.Errorf("the refresh changed the type: %q", refreshed.Type)
	}
	if refreshed.Description != fixture.Description {
		t.Errorf("the refresh changed the description: %q", refreshed.Description)
	}
	if refreshed.Captured == nil || refreshed.Captured.TxHash != testHash {
		t.Error("the refresh lost the capture provenance")
	}
	if strings.Contains(string(refreshed.Decoded), "stale") {
		t.Errorf("the decoded section was not refreshed:\n%s", refreshed.Decoded)
	}
}

// TestRefreshLeavesCurrentFixturesAlone checks that a refresh is safe to run
// over a whole testdata directory: a fixture that is already current is not
// rewritten, so the pull request stays empty.
func TestRefreshLeavesCurrentFixturesAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "envelope.golden.json")

	fixture, err := golden.Build(fixtureXDR(t, "tx_failed.env.txt"), "TransactionEnvelope", "current", nil)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if err := golden.Save(path, fixture); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	stdout, _, err := run(t, "refresh", dir)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if !strings.Contains(stdout, "unchanged") {
		t.Errorf("refresh did not report the fixture as unchanged:\n%s", stdout)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a current fixture was rewritten")
	}
}

// TestRefreshDryRunChangesNothing checks that the dry run shows what would
// change without touching a byte.
func TestRefreshDryRunChangesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "envelope.golden.json")

	fixture := staleFixture(t)
	if err := golden.Save(path, fixture); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	stdout, _, err := run(t, "refresh", dir, "--dry-run")
	if err != nil {
		t.Fatalf("refresh --dry-run failed: %v", err)
	}
	if !strings.Contains(stdout, "would change") {
		t.Errorf("the dry run did not say what would change:\n%s", stdout)
	}
	if !strings.Contains(stdout, "- ") || !strings.Contains(stdout, "+ ") {
		t.Errorf("the dry run did not show the difference:\n%s", stdout)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the dry run wrote to the fixture")
	}
}

// TestRefreshReportsAFixtureItCannotDecode checks that a broken fixture is
// reported rather than skipped: a refresh that quietly passes over a file it
// could not read is a refresh that leaves the file stale.
func TestRefreshReportsAFixtureItCannotDecode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.golden.json")

	fixture := staleFixture(t)
	fixture.XDR = "!!! not xdr !!!"
	if err := golden.Save(path, fixture); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	stdout, stderr, err := run(t, "refresh", dir)
	if err == nil {
		t.Fatal("refresh succeeded on a fixture it could not decode")
	}
	if code, _ := ExitCode(err); code != ExitFailure {
		t.Errorf("refresh exited %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(stderr, "ERROR") {
		t.Errorf("the broken fixture was not reported:\n%s%s", stdout, stderr)
	}
}

// staleFixture builds a fixture whose decoded section is out of date, as it
// is after the formatter changes.
func staleFixture(t *testing.T) *golden.Fixture {
	t.Helper()

	fixture, err := golden.Build(fixtureXDR(t, "tx_failed.env.txt"), "TransactionEnvelope", "stale",
		golden.NewProvenance(capture.NetworkTestnet, "https://fake.example", 1234, testHash,
			time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	fixture.Decoded = json.RawMessage(`{"stale": true}`)
	return fixture
}

func TestVersionCommand(t *testing.T) {
	stdout, _, err := run(t, "version")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Error("version printed nothing")
	}
}

func TestLedgerKeyAccountCommand(t *testing.T) {
	const address = "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF"
	wantKey, err := capture.NewAccountLedgerKey(address)
	if err != nil {
		t.Fatalf("NewAccountLedgerKey() error = %v", err)
	}
	want, err := capture.EncodeLedgerKey(wantKey)
	if err != nil {
		t.Fatalf("EncodeLedgerKey() error = %v", err)
	}

	stdout, _, err := run(t, "ledger-key", "account", address)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.TrimSpace(stdout) != want {
		t.Fatalf("ledger-key output = %q, want %q", strings.TrimSpace(stdout), want)
	}
}

func TestBareInvocationPrintsHelp(t *testing.T) {
	stdout, _, err := run(t)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	for _, want := range []string{"capture", "verify", "Usage"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help does not mention %q", want)
		}
	}
}

// TestLintAcceptsACleanFixture checks the ordinary case: a fixture nobody
// has touched is clean, and the command says so.
func TestLintAcceptsACleanFixture(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "envelope.golden.json")

	fixture, err := golden.Build(fixtureXDR(t, "tx_failed.env.txt"), "TransactionEnvelope", "clean", nil)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if err := golden.Save(path, fixture); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	stdout, _, err := run(t, "lint", dir)
	if err != nil {
		t.Fatalf("lint reported a problem with a clean fixture: %v", err)
	}
	if !strings.Contains(stdout, "ok") {
		t.Errorf("lint did not report the fixture as clean:\n%s", stdout)
	}
}

// TestLintReportsCorruptedFixtures is the reason the command exists: a
// fixture damaged by a hand-edit or a bad merge is caught here rather than
// when a test fails weeks later.
func TestLintReportsCorruptedFixtures(t *testing.T) {
	dir := t.TempDir()

	broken := map[string]string{
		"invalid-json.golden.json": `{"format": "focus/v1", "type": "TransactionEnvelope",}`,
		"bad-base64.golden.json":   `{"format": "focus/v1", "type": "TransactionEnvelope", "xdr": "!!! not base64 !!!"}`,
		"wrong-type.golden.json":   `{"format": "focus/v1", "type": "TransactionResult", "xdr": "AAAA"}`,
		"old-format.golden.json":   `{"format": "focus/v0", "type": "TransactionEnvelope", "xdr": "AAAA"}`,
	}
	for name, content := range broken {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	stdout, stderr, err := run(t, "lint", dir)
	if err == nil {
		t.Fatal("lint succeeded on corrupted fixtures")
	}
	if code, _ := ExitCode(err); code != ExitFailure {
		t.Errorf("lint exited %d, want %d", code, ExitFailure)
	}
	for _, name := range []string{"invalid-json", "bad-base64", "wrong-type", "old-format"} {
		if !strings.Contains(stderr, name) {
			t.Errorf("the broken fixture %s was not reported:\n%s%s", name, stdout, stderr)
		}
	}
	// The message must say what is wrong, not only that something is.
	if !strings.Contains(stderr, "not valid base64") {
		t.Errorf("the problems are not explained:\n%s", stderr)
	}
}

// TestLintReportsAnEmptyDirectory checks that the command says so rather
// than passing a check that never ran.
func TestLintReportsAnEmptyDirectory(t *testing.T) {
	_, _, err := run(t, "lint", t.TempDir())
	if err == nil {
		t.Fatal("lint succeeded with no fixtures to check")
	}
	if !strings.Contains(err.Error(), "no fixture files") {
		t.Errorf("error is not specific: %v", err)
	}
}
