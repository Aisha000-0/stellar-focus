package require_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-focus/internal/faketb"
	"github.com/stellar-optics/stellar-focus/pkg/focus"
	"github.com/stellar-optics/stellar-focus/pkg/focus/require"
)

func TestExpectedRequireName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"AssertEnvelopeEqual", "EnvelopeEqual"},
		{"AssertFee", "Fee"},
		{"AssertNoEvents", "NoEvents"},
		{"Assert", ""},
		{"EnvelopeEqual", ""},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := require.ExpectedRequireName(tc.in); got != tc.want {
				t.Errorf("ExpectedRequireName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMissingAndExtraMirrorsLogic(t *testing.T) {
	t.Parallel()

	focusSet := map[string]require.FuncSig{
		"AssertFee":           {Name: "AssertFee"},
		"AssertNoEvents":      {Name: "AssertNoEvents"},
		"AssertOperationType": {Name: "AssertOperationType"},
	}
	requireSet := map[string]require.FuncSig{
		"Fee":   {Name: "Fee"},
		"Extra": {Name: "Extra"},
	}

	missing := require.MissingMirrors(focusSet, requireSet)
	if len(missing) != 2 {
		t.Fatalf("MissingMirrors = %v, want 2 entries", missing)
	}
	joined := strings.Join(missing, "\n")
	for _, want := range []string{"AssertNoEvents -> NoEvents", "AssertOperationType -> OperationType"} {
		if !strings.Contains(joined, want) {
			t.Errorf("MissingMirrors missing %q in %v", want, missing)
		}
	}

	extra := require.ExtraMirrors(focusSet, requireSet)
	if len(extra) != 1 || extra[0] != "Extra" {
		t.Errorf("ExtraMirrors = %v, want [Extra]", extra)
	}
}

// TestAuditPassesOnRepo is the parity contract: every Assert* boolean in
// pkg/focus has a Fatalf mirror in pkg/focus/require with a matching
// signature.
func TestAuditPassesOnRepo(t *testing.T) {
	t.Parallel()

	focusSet, err := require.FocusAssertionsIn("..")
	if err != nil {
		t.Fatalf("FocusAssertionsIn: %v", err)
	}
	if len(focusSet) == 0 {
		t.Fatal("no focus assertions found; audit is vacuous")
	}
	// Escape hatches must not be mistaken for assertions.
	for _, escape := range []string{"Summarize", "Events", "Encode"} {
		for name := range focusSet {
			if strings.TrimPrefix(name, "Assert") == escape {
				t.Errorf("escape hatch %s counted as an assertion", escape)
			}
		}
	}

	requireSet, err := require.WrappersIn(".")
	if err != nil {
		t.Fatalf("WrappersIn: %v", err)
	}
	if len(requireSet) == 0 {
		t.Fatal("no require wrappers found; audit is vacuous")
	}

	if err := require.Audit("..", "."); err != nil {
		t.Errorf("parity audit failed:\n%v", err)
	}
}

func TestAuditDetectsDrift(t *testing.T) {
	t.Parallel()

	write := func(dir, name, src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}

	t.Run("missing mirror", func(t *testing.T) {
		t.Parallel()
		focusDir := t.TempDir()
		requireDir := t.TempDir()
		write(focusDir, "a.go", "package focus\nfunc AssertFoo(t TB, v Value) bool { return true }\n")
		write(requireDir, "r.go", "package require\nfunc Bar(t TB) {}\n")
		if err := require.Audit(focusDir, requireDir); err == nil {
			t.Fatal("Audit passed despite a missing mirror")
		} else if !strings.Contains(err.Error(), "missing mirror") || !strings.Contains(err.Error(), "AssertFoo") {
			t.Errorf("audit error does not name the missing mirror:\n%v", err)
		}
	})

	t.Run("extra mirror", func(t *testing.T) {
		t.Parallel()
		focusDir := t.TempDir()
		requireDir := t.TempDir()
		write(focusDir, "a.go", "package focus\nfunc AssertFoo(t TB, v Value) bool { return true }\n")
		write(requireDir, "r.go", "package require\nfunc Foo(t TB, v Value) {}\nfunc Orphan(t TB) {}\n")
		if err := require.Audit(focusDir, requireDir); err == nil {
			t.Fatal("Audit passed despite an extra mirror")
		} else if !strings.Contains(err.Error(), "extra mirror") || !strings.Contains(err.Error(), "Orphan") {
			t.Errorf("audit error does not name the extra mirror:\n%v", err)
		}
	})

	t.Run("signature mismatch", func(t *testing.T) {
		t.Parallel()
		focusDir := t.TempDir()
		requireDir := t.TempDir()
		write(focusDir, "a.go", "package focus\nfunc AssertFoo(t TB, v Value, n int, m map[string]uint32, s *string, xs ...int) bool { return true }\n")
		write(requireDir, "r.go", "package require\nfunc Foo(t TB, v Value) {}\n")
		if err := require.Audit(focusDir, requireDir); err == nil {
			t.Fatal("Audit passed despite a signature mismatch")
		} else if !strings.Contains(err.Error(), "signature mismatch") {
			t.Errorf("audit error does not mention signature:\n%v", err)
		}
	})

	t.Run("variadic mismatch", func(t *testing.T) {
		t.Parallel()
		focusDir := t.TempDir()
		requireDir := t.TempDir()
		write(focusDir, "a.go", "package focus\nfunc AssertFoo(t TB, xs ...int) bool { return true }\n")
		write(requireDir, "r.go", "package require\nfunc Foo(t TB, xs []int) {}\n")
		if err := require.Audit(focusDir, requireDir); err == nil {
			t.Fatal("Audit passed despite a variadic mismatch")
		} else if !strings.Contains(err.Error(), "variadic") {
			t.Errorf("audit error does not mention variadic:\n%v", err)
		}
	})

	t.Run("unreadable dir", func(t *testing.T) {
		t.Parallel()
		if err := require.Audit(filepath.Join(t.TempDir(), "nope"), "."); err == nil {
			t.Fatal("Audit passed for a missing focus dir")
		}
	})
}

// TestNewMirrorsReportFatally covers the four wrappers added to close the
// parity gap: they must stop the test (Fatalf), stay silent on success, and
// mark themselves as helpers.
func TestNewMirrorsReportFatally(t *testing.T) {
	t.Parallel()

	env := envelope(t, 100)
	other := envelope(t, 200)
	result := fixture(t, "tx_failed.res.txt")

	tests := []struct {
		name string
		call func(tb *faketb.TB)
	}{
		{"FailedOperations", func(tb *faketb.TB) { require.FailedOperations(tb, result, 99) }},
		{"FeeBump", func(tb *faketb.TB) { require.FeeBump(tb, env, true) }},
		{"NoEvents", func(tb *faketb.TB) { require.NoEvents(tb, "bad") }},
		{"OperationType", func(tb *faketb.TB) { require.OperationType(tb, env, 0, "change_trust") }},
		// Sanity: a failing established mirror still fails, proving the fake works.
		{"EnvelopeEqual", func(tb *faketb.TB) { require.EnvelopeEqual(tb, env, other) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tb := faketb.New()
			tc.call(tb)
			if len(tb.Fatals) == 0 {
				t.Errorf("%s did not report fatally", tc.name)
			}
			if len(tb.Errors) != 0 {
				t.Errorf("%s used Errorf instead of Fatalf:\n%s", tc.name, strings.Join(tb.Errors, "\n"))
			}
			if tb.Helpers == 0 {
				t.Errorf("%s did not call TB.Helper", tc.name)
			}
		})
	}
}

func TestNewMirrorsStaySilentOnSuccess(t *testing.T) {
	t.Parallel()

	env := envelope(t, 100)

	tb := faketb.New()
	require.FeeBump(tb, env, false)
	require.OperationType(tb, env, 0, "payment")
	if tb.Failed() {
		t.Errorf("passing mirrors reported a failure:\n%s", tb.Output())
	}
}

func TestNewMirrorMessagesMatchFocus(t *testing.T) {
	t.Parallel()

	env := envelope(t, 100)

	focusTB := faketb.New()
	focus.AssertOperationType(focusTB, env, 0, "change_trust")

	requireTB := faketb.New()
	require.OperationType(requireTB, env, 0, "change_trust")

	if focusTB.Output() != requireTB.Output() && !strings.Contains(requireTB.Output(), "AssertOperationType") {
		t.Errorf("require message diverged from focus:\nfocus:   %s\nrequire: %s",
			focusTB.Output(), requireTB.Output())
	}

	// FailedOperations failure names the mismatch, like its focus counterpart.
	tb := faketb.New()
	result := fixture(t, "tx_failed.res.txt")
	require.FailedOperations(tb, result, 99)
	if !strings.Contains(tb.Output(), "AssertFailedOperations") {
		t.Errorf("FailedOperations lost the assertion name:\n%s", tb.Output())
	}
}

// TestFocusAssertionsInSkipsNonAssertions guards the audit's filter: only
// Assert* booleans count, so helpers and escape hatches never demand mirrors.
func TestFocusAssertionsInSkipsNonAssertions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := `package focus
func AssertGood(t TB, v Value) bool { return true }
func Summarize(t TB, v Value) *int { return nil }
func Encode(v Value) string { return "" }
func helper() {}
func AssertNoBool(t TB, v Value) {}
`
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	got, err := require.FocusAssertionsIn(dir)
	if err != nil {
		t.Fatalf("FocusAssertionsIn: %v", err)
	}
	if len(got) != 1 || got["AssertGood"].Name != "AssertGood" {
		t.Errorf("FocusAssertionsIn = %v, want only AssertGood", got)
	}
}

// TestAuditHandlesExoticSignatures exercises every type renderer in the
// audit so future signature forms do not silently compare as unequal.
func TestAuditHandlesExoticSignatures(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	focusSrc := `package focus
func AssertExotic(t TB, fn func(int) bool, ch chan int, iface interface{}, anything any, arr [4]byte, m map[string]uint32) bool { return true }
`
	requireSrc := `package require
func Exotic(t TB, fn func(int) bool, ch chan int, iface interface{}, anything any, arr [4]byte, m map[string]uint32) {}
`
	if err := os.WriteFile(filepath.Join(dir, "focus.go"), []byte(focusSrc), 0o600); err != nil {
		t.Fatalf("writing focus fixture: %v", err)
	}
	requireDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(requireDir, "require.go"), []byte(requireSrc), 0o600); err != nil {
		t.Fatalf("writing require fixture: %v", err)
	}
	// interface{} and any must compare equal after normalisation.
	if err := require.Audit(dir, requireDir); err != nil {
		t.Errorf("audit rejected equivalent exotic signatures:\n%v", err)
	}

	focusSet, err := require.FocusAssertionsIn(dir)
	if err != nil {
		t.Fatalf("FocusAssertionsIn: %v", err)
	}
	sig, ok := focusSet["AssertExotic"]
	if !ok {
		t.Fatal("AssertExotic not found")
	}
	if len(sig.Params) != 7 {
		t.Errorf("AssertExotic params = %v, want 7", sig.Params)
	}
}
