package focus_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stellar-optics/stellar-focus/internal/faketb"
	"github.com/stellar-optics/stellar-focus/pkg/focus"
)

func TestColorizeAndStripANSI(t *testing.T) {
	t.Parallel()

	t.Run("colorize wraps with reset", func(t *testing.T) {
		t.Parallel()
		got := focus.Colorize("hello", focus.ANSIYellow)
		if !strings.HasPrefix(got, "\x1b[") || !strings.HasSuffix(got, "\x1b[0m") {
			t.Errorf("Colorize did not wrap with ANSI: %q", got)
		}
		if !focus.ContainsANSI(got) {
			t.Error("ContainsANSI missed a colorized string")
		}
		if stripped := focus.StripANSI(got); stripped != "hello" {
			t.Errorf("StripANSI = %q, want %q", stripped, "hello")
		}
	})

	t.Run("empty code disables colour", func(t *testing.T) {
		t.Parallel()
		if got := focus.Colorize("hello", ""); got != "hello" {
			t.Errorf("Colorize with empty code = %q, want unchanged", got)
		}
	})

	t.Run("plain text has no ANSI", func(t *testing.T) {
		t.Parallel()
		if focus.ContainsANSI("plain message") {
			t.Error("ContainsANSI reported colour in plain text")
		}
		if got := focus.StripANSI("plain message"); got != "plain message" {
			t.Errorf("StripANSI changed plain text: %q", got)
		}
	})

	t.Run("bold and highlight use ANSI", func(t *testing.T) {
		t.Parallel()
		for name, fn := range map[string]func(string) string{
			"bold":      focus.Bold,
			"highlight": focus.HighlightPath,
		} {
			got := fn("V1.Tx.SeqNum")
			if !focus.ContainsANSI(got) {
				t.Errorf("%s produced no ANSI: %q", name, got)
			}
			if stripped := focus.StripANSI(got); stripped != "V1.Tx.SeqNum" {
				t.Errorf("%s StripANSI = %q, want path", name, stripped)
			}
		}
	})
}

func TestColorEnabledModes(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"always", true},
		{"ALWAYS", true},
		{"never", false},
		{"", false},
		{"bogus", false},
	}
	for _, tc := range tests {
		t.Run("mode "+tc.mode, func(t *testing.T) {
			t.Parallel()
			if got := focus.ColorEnabled(tc.mode); got != tc.want {
				t.Errorf("ColorEnabled(%q) = %v, want %v", tc.mode, got, tc.want)
			}
		})
	}

	t.Run("auto honours NO_COLOR", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		t.Setenv("TERM", "xterm-256color")
		if focus.ColorEnabled("auto") {
			t.Error("ColorEnabled(auto) ignored NO_COLOR")
		}
	})

	t.Run("auto honours dumb terminal", func(t *testing.T) {
		t.Setenv("TERM", "dumb")
		if os.Getenv("NO_COLOR") != "" {
			t.Skip("NO_COLOR set in environment; covered above")
		}
		if focus.ColorEnabled("auto") {
			t.Error("ColorEnabled(auto) ignored TERM=dumb")
		}
	})
}

func TestFormatFieldMismatch(t *testing.T) {
	t.Parallel()

	t.Run("plain matches mismatch shape", func(t *testing.T) {
		t.Parallel()
		got := focus.FormatFieldMismatch("AssertFee", "fee", 200, 999, false)
		if focus.ContainsANSI(got) {
			t.Errorf("plain mismatch contains ANSI: %q", got)
		}
		for _, want := range []string{"AssertFee", "fee", "200", "999"} {
			if !strings.Contains(got, want) {
				t.Errorf("plain mismatch missing %q: %q", want, got)
			}
		}
	})

	t.Run("color highlights field and strips cleanly", func(t *testing.T) {
		t.Parallel()
		plain := focus.FormatFieldMismatch("AssertFee", "fee", 200, 999, false)
		colored := focus.FormatFieldMismatch("AssertFee", "fee", 200, 999, true)
		if !focus.ContainsANSI(colored) {
			t.Errorf("colored mismatch has no ANSI: %q", colored)
		}
		if stripped := focus.StripANSI(colored); stripped != plain {
			t.Errorf("stripped colored mismatch differs:\nplain:   %q\nstripped: %q", plain, stripped)
		}
		if !strings.Contains(colored, focus.HighlightPath("fee")) {
			t.Errorf("colored mismatch does not highlight the field:\n%s", colored)
		}
	})

	t.Run("empty and quoted strings stay visible", func(t *testing.T) {
		t.Parallel()
		got := focus.FormatFieldMismatch("AssertMemo", "memo", "", "text: hi", true)
		stripped := focus.StripANSI(got)
		if !strings.Contains(stripped, "(empty)") {
			t.Errorf("empty value not marked:\n%s", stripped)
		}
	})
}

func TestHighlightMismatchField(t *testing.T) {
	t.Parallel()

	t.Run("wraps the mismatched field", func(t *testing.T) {
		t.Parallel()
		msg := "AssertEnvelopeEqual: envelopes differ — 1 difference\n  ~ V1.Tx.SeqNum"
		got := focus.HighlightMismatchField(msg, "V1.Tx.SeqNum")
		if !focus.ContainsANSI(got) {
			t.Errorf("no ANSI added:\n%s", got)
		}
		if stripped := focus.StripANSI(got); stripped != msg {
			t.Errorf("stripped message changed:\n%q vs %q", stripped, msg)
		}
	})

	t.Run("empty field leaves message alone", func(t *testing.T) {
		t.Parallel()
		msg := "something failed"
		if got := focus.HighlightMismatchField(msg, ""); got != msg {
			t.Errorf("empty field changed the message: %q", got)
		}
		if got := focus.HighlightMismatchField(msg, "absent"); got != msg {
			t.Errorf("absent field changed the message: %q", got)
		}
	})
}

func TestTerminalAssertionOutputViaFakeTB(t *testing.T) {
	t.Parallel()

	want := buildEnvelopeSeq(t, 1, paymentOp(t, 100))
	got := buildEnvelopeSeq(t, 1, paymentOp(t, 200))

	t.Run("default output has no colour", func(t *testing.T) {
		t.Parallel()
		tb := faketb.New()
		if focus.AssertEnvelopeEqual(tb, want, got) {
			t.Fatal("assertion passed for differing envelopes")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if focus.ContainsANSI(msg) {
			t.Errorf("default failure contains ANSI:\n%s", msg)
		}
		if !strings.Contains(msg, "Body.PaymentOp.Amount") {
			t.Errorf("failure does not name the mismatched field:\n%s", msg)
		}
	})

	t.Run("always colours the mismatched field", func(t *testing.T) {
		t.Parallel()
		tb := faketb.New()
		if focus.AssertEnvelopeEqual(tb, want, got, focus.WithColor("always")) {
			t.Fatal("assertion passed for differing envelopes")
		}
		msg, err := tb.Message()
		if err != nil {
			t.Fatalf("%v", err)
		}
		if !focus.ContainsANSI(msg) {
			t.Errorf("WithColor(always) produced no ANSI:\n%s", msg)
		}
		// The colour must survive stripping to the same field path.
		stripped := focus.StripANSI(msg)
		if !strings.Contains(stripped, "Body.PaymentOp.Amount") {
			t.Errorf("stripped failure lost the field path:\n%s", stripped)
		}
		// Highlighting exactly the mismatched field must be idempotent:
		// re-highlighting the stripped message reproduces colour without
		// changing text.
		recolored := focus.HighlightMismatchField(stripped, "Body.PaymentOp.Amount")
		if focus.StripANSI(recolored) != stripped {
			t.Error("highlighting changed the plain text")
		}
	})
}

func TestWriterPaletteHelpers(t *testing.T) {
	t.Parallel()

	t.Run("non-terminal writer stays plain", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		if focus.SupportsColorWriter(&buf) {
			t.Error("SupportsColorWriter reported colour for a buffer")
		}
		if got := focus.PaletteForWriter(&buf, "never"); got.Reset != "" {
			t.Errorf("never palette carries colour: %+v", got)
		}
		if got := focus.PaletteForWriter(&buf, "always"); got.Reset == "" {
			t.Errorf("always palette has no reset: %+v", got)
		}
	})

	t.Run("auto honours NO_COLOR for writers", func(t *testing.T) {
		var buf bytes.Buffer
		p := focus.PaletteForWriter(&buf, "auto")
		// A buffer is never a terminal, so auto must be plain regardless of
		// the environment; this keeps CI logs clean.
		if p.Reset != "" {
			t.Errorf("auto palette for a buffer carries colour: %+v", p)
		}
	})
}
