package focus

import (
	"fmt"
	"sort"
	"strings"

	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	lensformat "github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

// Option configures an assertion.
type Option func(*config)

// config holds resolved assertion settings.
type config struct {
	// ignore lists path patterns whose differences are not failures.
	ignore []string
	// sorters are path patterns whose arrays are treated as unordered sets.
	// When enabled, arrays at matching paths are sorted before the comparison
	// so that [sigA, sigB] and [sigB, sigA] are equal.
	sorters []sorter
	// normalize maps a path pattern to a function rewriting matching values
	// before comparison.
	normalize []normalizer
	// colorMode is "auto", "always" or "never", matching lens.
	colorMode string
	// maxChanges caps how many differences are printed before the rest are
	// summarised.
	maxChanges int
}

type normalizer struct {
	pattern string
	fn      func(any) any
}

// sorter describes one array treated as an unordered set.
type sorter struct {
	pattern string
	// keyFn extracts the lexicographic key of an element. When nil, the
	// element's canonical rendering is used, which orders elements as wholes.
	keyFn func(elem any) string
}

func newConfig(opts ...Option) *config {
	c := &config{colorMode: "never", maxChanges: 20}
	for _, o := range opts {
		o(c)
	}
	return c
}

// IgnorePaths excludes differences at matching paths from the comparison.
//
// Patterns match the dotted paths lens reports, such as
// "V1.Tx.Operations[0].Body.PaymentOp.Amount".
//
// A "*" segment matches exactly one path segment, and a "**" segment matches
// any number of segments at any depth. An index may also be wildcarded, as in
// "Operations[*]". Within a segment, "*", "?" and character classes work like
// globs. Matching is case-insensitive, and a pattern is anchored to the tail
// of a path as well as its whole, so none of these need a "V1.Tx." prefix:
//
//	focus.IgnorePaths("**.Signatures")                  // signatures at any depth
//	focus.IgnorePaths("operations[*].sourceaccount")    // per-operation source
//	focus.IgnorePaths("V1.Tx.Operations[*].SourceAccount")
//
// Ignored paths are always named in the failure output. A silently discarded
// difference is how an assertion library stops being trustworthy.
func IgnorePaths(patterns ...string) Option {
	return func(c *config) { c.ignore = append(c.ignore, patterns...) }
}

// IgnoreSequence ignores the transaction sequence number, which changes on
// every submission and is almost never what a test is asserting.
func IgnoreSequence() Option {
	return IgnorePaths("**.SeqNum")
}

// IgnoreSignatures ignores signatures, which differ whenever a test signs
// with a freshly generated key.
func IgnoreSignatures() Option {
	return IgnorePaths("**.Signatures")
}

// IgnoreTimeBounds ignores time bounds, which move with wall-clock time.
func IgnoreTimeBounds() Option {
	return IgnorePaths("**.TimeBounds", "**.MinTime", "**.MaxTime")
}

// IgnoreFee ignores the fee bid and the fee charged.
func IgnoreFee() Option {
	return IgnorePaths("**.Fee", "**.FeeCharged")
}

// IgnoreSourceAccount ignores the transaction and operation source accounts,
// for tests that generate a funding account per run.
func IgnoreSourceAccount() Option {
	return IgnorePaths("**.SourceAccount")
}

// IgnoreVolatile is the common combination: sequence numbers, signatures and
// time bounds. It is what most tests comparing a rebuilt transaction want.
func IgnoreVolatile() Option {
	return func(c *config) {
		IgnoreSequence()(c)
		IgnoreSignatures()(c)
		IgnoreTimeBounds()(c)
	}
}

// NormalizePath rewrites values at matching paths before they are compared,
// for cases an ignore rule is too blunt for.
//
// The function receives the decoded scalar and returns the value to compare,
// so a timestamp can be rounded or an address mapped to a placeholder rather
// than dropped entirely.
func NormalizePath(pattern string, fn func(any) any) Option {
	return func(c *config) {
		c.normalize = append(c.normalize, normalizer{pattern: pattern, fn: fn})
	}
}

// SortPath tells the comparison to treat the array at a matching path as an
// unordered set.
//
// XDR arrays are ordered for a reason — operations run in the order they
// appear — but a few are not: the signatures on a multi-signed transaction
// arrive in whichever order the signers signed in, and the keys in a Soroban
// footprint are a set. Comparing those positionally reports one difference per
// element for what is really the same value.
//
//	focus.SortPath("**.Signatures")
//
// By default an element's whole canonical rendering is its key, which is
// enough when elements are compared as wholes. When the array has a natural
// identity, keyFn names it: it receives the decoded element as a *lens.Node
// and returns the string to sort by.
//
//	focus.SortPath("**.Signers", func(elem any) string {
//	    n, _ := elem.(*lens.Node)
//	    return n.Child("Key").String()
//	})
//
// The arrays at the matching paths are sorted before the two values are
// diffed, so [sigA, sigB] and [sigB, sigA] compare equal. Everything else is
// still compared the usual way: a missing signature is still a difference.
//
// The paths that were treated as unordered sets are named in the failure
// message, and a pattern that matches no array at all is reported as
// probably stale, because a rule that quietly does nothing is how a test
// starts passing for the wrong reason.
func SortPath(pattern string, keyFn ...func(elem any) string) Option {
	return func(c *config) {
		s := sorter{pattern: pattern}
		if len(keyFn) > 0 {
			s.keyFn = keyFn[0]
		}
		c.sorters = append(c.sorters, s)
	}
}

// SortSigners treats the signers on an account as an unordered set.
//
// A signer list is a set: which key has which weight is meaningful, but the
// order the entries happen to be stored in is not.
func SortSigners() Option {
	return SortPath("**.Signers")
}

// SortFootprint treats the read-only and read-write keys of a Soroban ledger
// footprint as unordered sets.
func SortFootprint() Option {
	return func(c *config) {
		SortPath("**.Footprint.ReadOnly")(c)
		SortPath("**.Footprint.ReadWrite")(c)
	}
}

// sortResult records what the sort rules did, so the failure message can say
// how the two values were compared.
type sortResult struct {
	// unordered names the array paths that were compared as unordered sets.
	unordered []string
	// stale names the patterns that matched no array in either value.
	stale []string
}

// sortValues returns the values to compare, with the arrays at the matching
// paths sorted in both of them.
//
// The values are copied rather than sorted in place. A caller may hand in a
// decoded value it still holds — a *lens.Value is an accepted input — and an
// assertion must not reorder somebody else's data.
func (c *config) sortValues(want, got *lens.Value) (*lens.Value, *lens.Value, sortResult) {
	if len(c.sorters) == 0 {
		return want, got, sortResult{}
	}

	wantCopy := &lens.Value{Type: want.Type, Node: cloneNode(want.Node), Raw: want.Raw}
	gotCopy := &lens.Value{Type: got.Type, Node: cloneNode(got.Node), Raw: got.Raw}

	var unordered []string
	unordered = append(unordered, sortNode(wantCopy.Node, "", c.sorters)...)
	unordered = append(unordered, sortNode(gotCopy.Node, "", c.sorters)...)

	paths := listPaths(wantCopy.Node, "", nil)
	paths = listPaths(gotCopy.Node, "", paths)

	return wantCopy, gotCopy, sortResult{
		unordered: dedupe(unordered),
		stale:     c.unusedSorts(paths),
	}
}

// sortNode sorts the children of every list at a path matching a sorter, and
// returns the paths it sorted.
//
// The order is by the sorter's key function, or by canonical rendering when
// it has none, because an unordered array has no field that identifies its
// elements unless one is supplied: the elements are compared as wholes.
func sortNode(n *lens.Node, path string, sorters []sorter) []string {
	if n == nil {
		return nil
	}

	var sorted []string
	switch n.Kind {
	case lens.KindList:
		for _, s := range sorters {
			if !matchPath(s.pattern, path) {
				continue
			}
			sort.SliceStable(n.Children, func(i, j int) bool {
				return sortKey(n.Children[i], s.keyFn) < sortKey(n.Children[j], s.keyFn)
			})
			sorted = append(sorted, path)
		}
		for i, child := range n.Children {
			sorted = append(sorted, sortNode(child, fmt.Sprintf("%s[%d]", path, i), sorters)...)
		}
	case lens.KindStruct:
		for _, child := range n.Children {
			sorted = append(sorted, sortNode(child, joinPath(path, child.Name), sorters)...)
		}
	}
	return sorted
}

// sortKey extracts the sort key of one array element.
//
// A key function is user code, and assertions do not panic: if it panics or
// returns nothing useful, the element's canonical rendering is used instead so
// the comparison can still be made.
func sortKey(n *lens.Node, keyFn func(any) string) (key string) {
	if keyFn == nil {
		return canonicalKey(n)
	}
	defer func() {
		if recover() != nil {
			key = canonicalKey(n)
		}
	}()
	return keyFn(n)
}

// listPaths collects the paths of every list in a tree.
//
// This is what the stale-pattern check looks at: a sort pattern that matches
// no list matched nothing.
func listPaths(n *lens.Node, path string, out []string) []string {
	if n == nil {
		return out
	}

	switch n.Kind {
	case lens.KindList:
		out = append(out, path)
		for i, child := range n.Children {
			out = listPaths(child, fmt.Sprintf("%s[%d]", path, i), out)
		}
	case lens.KindStruct:
		for _, child := range n.Children {
			out = listPaths(child, joinPath(path, child.Name), out)
		}
	}
	return out
}

// unusedSorts reports sort patterns that matched no array.
//
// A pattern that never matches is usually a typo — a stale field name, or a
// path with the wrong prefix — and silently doing nothing is exactly how a
// test starts passing for the wrong reason.
func (c *config) unusedSorts(paths []string) []string {
	var unused []string
	for _, s := range c.sorters {
		var hit bool
		for _, path := range paths {
			if matchPath(s.pattern, path) {
				hit = true
				break
			}
		}
		if !hit {
			unused = append(unused, s.pattern)
		}
	}
	return unused
}

// canonicalKey renders a subtree into a stable string, so two elements of an
// unordered array can be ordered by something other than their position.
//
// Every part of the element takes part — the field names, the values and the
// enriched notes — because an element is compared as a whole.
func canonicalKey(n *lens.Node) string {
	var b strings.Builder
	writeCanonicalKey(&b, n)
	return b.String()
}

func writeCanonicalKey(b *strings.Builder, n *lens.Node) {
	if n == nil {
		b.WriteString("nil;")
		return
	}

	switch n.Kind {
	case lens.KindScalar:
		fmt.Fprintf(b, "s(%v|%s);", n.Value, n.Note)
	case lens.KindList:
		b.WriteString("l[")
		for _, child := range n.Children {
			writeCanonicalKey(b, child)
		}
		b.WriteString("];")
	default:
		b.WriteString("t{")
		for _, child := range n.Children {
			fmt.Fprintf(b, "%s=", child.Name)
			writeCanonicalKey(b, child)
		}
		b.WriteString("};")
	}
}

// cloneNode copies a tree, so sorting a copy leaves the caller's value alone.
func cloneNode(n *lens.Node) *lens.Node {
	if n == nil {
		return nil
	}

	clone := &lens.Node{
		Name:     n.Name,
		Kind:     n.Kind,
		TypeName: n.TypeName,
		Value:    n.Value,
		Note:     n.Note,
	}
	if len(n.Children) > 0 {
		clone.Children = make([]*lens.Node, len(n.Children))
		for i, child := range n.Children {
			clone.Children[i] = cloneNode(child)
		}
	}
	return clone
}

// joinPath appends a field name to a path.
func joinPath(path, name string) string {
	switch {
	case name == "":
		return path
	case path == "":
		return name
	default:
		return path + "." + name
	}
}

// dedupe removes repeated entries, keeping the order they came in.
func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}

	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// WithColor sets whether the failure diff is colourised: "auto", "always" or
// "never", matching lens. The default is "never", because test output is
// usually captured rather than read on a terminal.
func WithColor(mode string) Option {
	return func(c *config) { c.colorMode = mode }
}

// WithMaxChanges caps how many individual differences appear in a failure
// message before the remainder are summarised as a count. A value of 0 or
// less prints all of them.
func WithMaxChanges(n int) Option {
	return func(c *config) { c.maxChanges = n }
}

// palette resolves the colour palette for failure output.
func (c *config) palette() lensformat.Palette {
	switch c.colorMode {
	case "always":
		return lensformat.ColorPalette
	default:
		// "auto" behaves as "never": an assertion writes through TB.Errorf,
		// which is captured by the test framework rather than written to a
		// terminal, so there is nothing to detect.
		return lensformat.NoColor
	}
}

// filter removes ignored changes from a diff and reports which patterns
// actually matched something.
//
// Returning the matched patterns rather than just the filtered list is what
// lets the failure message say what was ignored, and lets a caller notice
// that an ignore rule matched nothing.
func (c *config) filter(changes []lens.Change) (kept []lens.Change, applied []string) {
	if len(c.ignore) == 0 {
		return changes, nil
	}

	matched := make(map[string]bool, len(c.ignore))
	kept = make([]lens.Change, 0, len(changes))

	for _, ch := range changes {
		var skip bool
		for _, pattern := range c.ignore {
			if matchPath(pattern, ch.Path) {
				skip = true
				matched[pattern] = true
				break
			}
		}
		if !skip {
			kept = append(kept, ch)
		}
	}

	for _, pattern := range c.ignore {
		if matched[pattern] {
			applied = append(applied, pattern)
		}
	}
	return kept, applied
}

// unusedIgnores reports ignore patterns that matched nothing.
//
// A pattern that never matches is usually a typo — a stale field name, or a
// path with the wrong prefix — and silently doing nothing is exactly how a
// test starts passing for the wrong reason.
func (c *config) unusedIgnores(changes []lens.Change) []string {
	var unused []string
	for _, pattern := range c.ignore {
		var hit bool
		for _, ch := range changes {
			if matchPath(pattern, ch.Path) {
				hit = true
				break
			}
		}
		if !hit {
			unused = append(unused, pattern)
		}
	}
	return unused
}
