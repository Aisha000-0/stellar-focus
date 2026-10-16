package focus_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-focus/internal/faketb"
	"github.com/stellar-optics/stellar-focus/pkg/focus"
)

func TestSafeExecPassThrough(t *testing.T) {
	t.Parallel()

	t.Run("true is returned without a failure", func(t *testing.T) {
		t.Parallel()
		tb := faketb.New()
		if !focus.SafeExec(tb, "AssertTest", func() bool { return true }) {
			t.Error("SafeExec returned false for a non-panicking true")
		}
		if tb.Failed() {
			t.Errorf("SafeExec reported a failure for a passing function:\n%s", tb.Output())
		}
		if tb.Helpers == 0 {
			t.Error("SafeExec did not call TB.Helper")
		}
	})

	t.Run("false is returned without a panic message", func(t *testing.T) {
		t.Parallel()
		tb := faketb.New()
		if focus.SafeExec(tb, "AssertTest", func() bool { return false }) {
			t.Error("SafeExec returned true for a function returning false")
		}
		if tb.Failed() {
			t.Errorf("SafeExec reported a panic failure for a plain false:\n%s", tb.Output())
		}
	})
}

func TestSafeExecRecoversPanics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		panicker  func() bool
		wantInMsg []string
	}{
		{
			name:      "string panic",
			panicker:  func() bool { panic("boom") },
			wantInMsg: []string{"AssertTest", "internal error", "boom", "bug in stellar-focus"},
		},
		{
			name:      "error panic",
			panicker:  func() bool { panic(errors.New("kaboom")) },
			wantInMsg: []string{"AssertTest", "kaboom", "bug in stellar-focus"},
		},
		{
			name:      "int panic",
			panicker:  func() bool { panic(42) },
			wantInMsg: []string{"AssertTest", "42"},
		},
		{
			name: "nil dereference",
			panicker: func() bool {
				var p *int
				return *p == 0
			},
			wantInMsg: []string{"AssertTest", "internal error"},
		},
		{
			name: "slice out of range",
			panicker: func() bool {
				s := []int{}
				return s[9] == 0
			},
			wantInMsg: []string{"AssertTest", "internal error"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tb := faketb.New()
			if focus.SafeExec(tb, "AssertTest", tc.panicker) {
				t.Error("SafeExec returned true despite a panic")
			}
			msg, err := tb.Message()
			if err != nil {
				t.Fatalf("%v\noutput:\n%s", err, tb.Output())
			}
			for _, want := range tc.wantInMsg {
				if !strings.Contains(msg, want) {
					t.Errorf("panic message does not contain %q:\n%s", want, msg)
				}
			}
			if tb.Helpers == 0 {
				t.Error("SafeExec did not call TB.Helper on the panic path")
			}
		})
	}
}

func TestSafeCall(t *testing.T) {
	t.Parallel()

	t.Run("completing call returns true silently", func(t *testing.T) {
		t.Parallel()
		tb := faketb.New()
		called := false
		if !focus.SafeCall(tb, "AssertTest", func() { called = true }) {
			t.Error("SafeCall returned false for a non-panicking function")
		}
		if !called {
			t.Error("SafeCall did not run the function")
		}
		if tb.Failed() {
			t.Errorf("SafeCall reported a failure:\n%s", tb.Output())
		}
	})

	t.Run("panicking call returns false with a failure", func(t *testing.T) {
		t.Parallel()
		tb := faketb.New()
		if focus.SafeCall(tb, "AssertCall", func() { panic("call boom") }) {
			t.Error("SafeCall returned true despite a panic")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		for _, want := range []string{"AssertCall", "call boom", "bug in stellar-focus"} {
			if !strings.Contains(msg, want) {
				t.Errorf("message does not contain %q:\n%s", want, msg)
			}
		}
	})
}

func TestSafeExecErrorWrapping(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("sentinel cause")
	tb := faketb.New()
	focus.SafeExec(tb, "AssertWrap", func() bool { panic(sentinel) })
	msg, err := tb.Message()
	if err != nil {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(msg, "sentinel cause") {
		t.Errorf("wrapped panic message lost the cause:\n%s", msg)
	}
}

func TestSafeExecInsideAssertions(t *testing.T) {
	t.Parallel()

	// A hostile input that would panic without a guard must surface as a
	// regular failure, whether driven directly or through the harness.
	hostile := []byte{0x00}
	tb := faketb.New()
	if focus.SafeExec(tb, "AssertEnvelopeEqual", func() bool {
		return focus.AssertEnvelopeEqual(tb, hostile, hostile)
	}) {
		t.Error("expected the wrapped assertion to fail on hostile input")
	}
	if !tb.Failed() {
		t.Error("expected a failure to be recorded for hostile input")
	}
}

func TestPanicErrorMethods(t *testing.T) {
	t.Parallel()

	t.Run("error string names assertion and cause", func(t *testing.T) {
		t.Parallel()
		err := &focus.PanicError{Assertion: "AssertFoo", Recovered: "boom"}
		msg := err.Error()
		for _, want := range []string{"AssertFoo", "boom", "internal error"} {
			if !strings.Contains(msg, want) {
				t.Errorf("PanicError.Error missing %q: %q", want, msg)
			}
		}
	})

	t.Run("unwrap exposes error causes for errors.Is", func(t *testing.T) {
		t.Parallel()
		sentinel := errors.New("sentinel")
		withErr := &focus.PanicError{Assertion: "A", Recovered: sentinel}
		if !errors.Is(withErr, sentinel) {
			t.Error("errors.Is did not see through PanicError to the cause")
		}
		withoutErr := &focus.PanicError{Assertion: "A", Recovered: "not an error"}
		if errors.Is(withoutErr, sentinel) {
			t.Error("errors.Is matched a non-error recovery")
		}
		var target *focus.PanicError
		if !errors.As(withErr, &target) {
			t.Error("errors.As did not find the PanicError")
		}
	})

	t.Run("stack snippet truncates", func(t *testing.T) {
		t.Parallel()
		err := &focus.PanicError{Assertion: "A", Stack: []byte("0123456789")}
		if got := err.StackSnippet(0); got != "0123456789" {
			t.Errorf("StackSnippet(0) = %q, want full stack", got)
		}
		if got := err.StackSnippet(100); got != "0123456789" {
			t.Errorf("StackSnippet(large) = %q, want full stack", got)
		}
		got := err.StackSnippet(4)
		if !strings.HasPrefix(got, "0123") || !strings.Contains(got, "truncated") {
			t.Errorf("StackSnippet(4) did not truncate: %q", got)
		}
	})
}
