package focus

import (
	"testing"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
)

func TestMatchPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		// Exact and segment wildcards.
		{"V1.Tx.SeqNum", "V1.Tx.SeqNum", true},
		{"V1.Tx.SeqNum", "V1.Tx.SeqNum2", false},
		{"V1.Tx.SeqNum", "V1.Tx.Signatures[0].Signature", false},
		{"V1.Tx.*", "V1.Tx.SeqNum", true},
		{"V1.Tx.*", "V1.Tx.Operations[0]", true},
		{"V1.Tx.*", "V1.Tx.Body.Op", false},
		{"*", "V1", true},
		// Tail anchoring: "*" is the last segment, not the whole path.
		{"*", "V1.Tx", true},
		{"*.Tx", "V1.Tx", true},
		{"*.Ops", "V1.Tx", false},

		// "**" matches at any depth, including no segments at all.
		{"**.SeqNum", "V1.Tx.SeqNum", true},
		{"**.SeqNum", "SeqNum", true},
		{"**.SeqNum", "V1.Tx.Operations[0].Body.SeqNum", true},
		{"**.SeqNum", "V1.Tx.SeqNum.Ext", false},
		{"**", "V1.Tx.SeqNum", true},
		{"V1.**.SeqNum", "V1.Tx.Operations[0].SeqNum", true},

		// Index wildcards and bare names over indexed segments.
		{"**.Operations[*]", "V1.Tx.Operations[0]", true},
		{"**.Operations[*]", "V1.Tx.Operations[12]", true},
		{"**.Operations[*].SourceAccount", "V1.Tx.Operations[7].SourceAccount", true},
		{"**.Operations[0].SourceAccount", "V1.Tx.Operations[1].SourceAccount", false},
		{"**.Signatures", "V1.Tx.Signatures[0]", true},
		{"**.Operations[1*]", "V1.Tx.Operations[12]", true},

		// Globs within a segment.
		{"**.Seq*", "V1.Tx.SeqNum", true},
		{"**.Seq*", "V1.Tx.Signatures", false},
		{"**.Source*", "V1.Tx.Operations[0].SourceAccount", true},
		{"**.Fee*", "V1.Tx.FeeCharged", true},
		{"V?.Tx.SeqNum", "V1.Tx.SeqNum", true},
		{"V?.Tx.SeqNum", "V10.Tx.SeqNum", false},
		{"**.Seq[Nn]um", "V1.Tx.SeqNum", true},
		{"**.Seq[!N]um", "V1.Tx.SeqNum", false},
		{"**.Seq[!X]um", "V1.Tx.SeqNum", true},
		{"**.Operations[[0-9]]", "V1.Tx.Operations[7]", true},
		{"**.Operations[[0-9]]", "V1.Tx.Operations[12]", false},
		{"**.Operations[!0]", "V1.Tx.Operations[1]", true},
		{"**.Operations[!0]", "V1.Tx.Operations[0]", false},

		// Case-insensitive, so patterns may be written the way fields read.
		{"operations[*].sourceaccount", "V1.Tx.Operations[0].SourceAccount", true},
		{"**.SEQNUM", "V1.Tx.SeqNum", true},
		{"**.seqnum", "V1.Tx.SeqNum", true},

		// A malformed class is a literal, not an error.
		{"**.Seq[Num", "V1.Tx.Seq[Num", true},
		{"**.Seq[Num", "V1.Tx.SeqNum", false},
	}

	for _, tc := range tests {
		t.Run(tc.pattern+"__"+tc.path, func(t *testing.T) {
			t.Parallel()

			if got := matchPath(tc.pattern, tc.path); got != tc.want {
				t.Errorf("matchPath(%q, %q) = %t, want %t", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

func TestGlobMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		value   string
		want    bool
	}{
		{"", "", true},
		{"", "a", false},
		{"a", "a", true},
		{"a", "A", true},
		{"*", "", true},
		{"*", "anything", true},
		{"a*", "abc", true},
		{"*c", "abc", true},
		{"a*c", "abc", true},
		{"a*c", "ac", true},
		{"a*c", "abd", false},
		{"a**c", "abc", true},
		{"a?c", "abc", true},
		{"a?c", "abbc", false},
		{"a?c", "ac", false},
		{"[abc]", "b", true},
		{"[abc]", "d", false},
		{"[a-c]bc", "abc", true},
		{"[!a]bc", "abc", false},
		{"[!a]bc", "xbc", true},
		{"[^a]bc", "xbc", true},
		{"a-", "a-", true},
		{"[a-]", "-", true},
		{"[a-]", "a", true},
		{"[]]", "]", true},
		{"[!]]", "]", false},
		{"[!]]", "x", true},
		{"[", "[", true},
		{"[", "x", false},
		{"[abc", "[abc", true},
	}

	for _, tc := range tests {
		t.Run(tc.pattern+"__"+tc.value, func(t *testing.T) {
			t.Parallel()

			if got := globMatch(tc.pattern, tc.value); got != tc.want {
				t.Errorf("globMatch(%q, %q) = %t, want %t", tc.pattern, tc.value, got, tc.want)
			}
		})
	}
}

// TestIgnoreRulesFilterChanges exercises the ignore pipeline with the kind of
// changes lens actually reports, so the matcher is tested against real dotted
// paths rather than only hand-picked strings.
func TestIgnoreRulesFilterChanges(t *testing.T) {
	t.Parallel()

	changes := []lens.Change{
		{Path: "V1.Tx.SeqNum"},
		{Path: "V1.Tx.Operations[0].SourceAccount"},
		{Path: "V1.Tx.Operations[1].SourceAccount"},
		{Path: "V1.Tx.Operations[0].Body.PaymentOp.Amount"},
		{Path: "V1.Tx.FeeCharged"},
	}

	tests := []struct {
		name        string
		pattern     string
		wantKept    []string
		wantApplied bool
	}{
		{
			name:        "sequence",
			pattern:     "**.seqnum",
			wantKept:    []string{"V1.Tx.Operations[0].SourceAccount", "V1.Tx.Operations[1].SourceAccount", "V1.Tx.Operations[0].Body.PaymentOp.Amount", "V1.Tx.FeeCharged"},
			wantApplied: true,
		},
		{
			name:        "operation sources by glob",
			pattern:     "operations[*].source*",
			wantKept:    []string{"V1.Tx.SeqNum", "V1.Tx.Operations[0].Body.PaymentOp.Amount", "V1.Tx.FeeCharged"},
			wantApplied: true,
		},
		{
			name:        "fee fields by glob",
			pattern:     "**.Fee*",
			wantKept:    []string{"V1.Tx.SeqNum", "V1.Tx.Operations[0].SourceAccount", "V1.Tx.Operations[1].SourceAccount", "V1.Tx.Operations[0].Body.PaymentOp.Amount"},
			wantApplied: true,
		},
		{
			name:        "stale pattern matches nothing",
			pattern:     "**.NoSuchField",
			wantKept:    []string{"V1.Tx.SeqNum", "V1.Tx.Operations[0].SourceAccount", "V1.Tx.Operations[1].SourceAccount", "V1.Tx.Operations[0].Body.PaymentOp.Amount", "V1.Tx.FeeCharged"},
			wantApplied: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := newConfig(IgnorePaths(tc.pattern))
			kept, applied := c.filter(changes)

			if len(kept) != len(tc.wantKept) {
				t.Fatalf("kept %d change(s), want %d", len(kept), len(tc.wantKept))
			}
			for i, want := range tc.wantKept {
				if kept[i].Path != want {
					t.Errorf("kept[%d] = %q, want %q", i, kept[i].Path, want)
				}
			}
			if len(applied) > 0 != tc.wantApplied {
				t.Errorf("applied = %v, want applied=%t", applied, tc.wantApplied)
			}
		})
	}
}
