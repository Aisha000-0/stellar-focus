package focus

import (
	"github.com/stellar-optics/stellar-xdr-lens/pkg/lens"
	lensformat "github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"
)

// Option configures an assertion.
type Option func(*config)

// config holds resolved assertion settings.
type config struct {
	// ignore lists path patterns whose differences are not failures.
	ignore []string
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
