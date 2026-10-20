package cli

// The verify command: re-check stored fixtures against the live network.
//
// Split from cli.go so the command that the library exists for — the one
// that tells you a fixture has gone stale — has a file of its own to grow
// in, with its tests beside it.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	lensformat "github.com/stellar-optics/stellar-xdr-lens/pkg/lens/format"

	"github.com/stellar-optics/stellar-focus/pkg/capture"
	"github.com/stellar-optics/stellar-focus/pkg/golden"
)

// defaultVerifyDir is what `focus verify` checks when given no paths.
const defaultVerifyDir = "testdata"

func newVerifyCmd(cfg *config, stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify [paths...]",
		Short: "Check that stored fixtures still match the chain",
		Long: `Re-fetch each fixture from the network and report any that no longer match.

Only fixtures carrying capture provenance can be checked; hand-written ones
are skipped and reported as such rather than silently passing.

Paths may be files or directories. With no path, testdata is checked.

A repository with hundreds of fixtures does not need all of them re-fetched on
every commit, so a run can be narrowed:

  --since <duration|RFC3339>   only check fixtures captured after a threshold
  --filter <pattern>           only check fixtures matching a glob or a
                               metadata selector such as network:testnet
  --drifted-only               only re-check fixtures that previously drifted
                               or could not be checked

What each run found is cached under .focus, so --drifted-only knows what to
re-check. The cache is a speed-up, never a source of truth.

Exit status is 0 when every checked fixture still matches, 2 when at least one
has drifted, and 1 when a fixture could not be checked at all.`,
		Example: `  focus verify
  focus verify testdata/ --network mainnet
  focus verify testdata/ --since 48h
  focus verify testdata/ --filter 'envelope*'
  focus verify testdata/ --filter network:testnet
  focus verify testdata/ --drifted-only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The settings are read before anything is loaded or fetched, so
			// an unusable one fails before the run has done any work.
			since, err := parseSince(cfg.since(), now())
			if err != nil {
				return usageErrorf("%v", err)
			}
			filter, err := parseFilter(cfg.filter())
			if err != nil {
				return usageErrorf("%v", err)
			}

			if len(args) == 0 {
				args = []string{defaultVerifyDir}
			}

			paths, err := collectFixtures(args)
			if err != nil {
				return failureErrorf("%v", err)
			}
			if len(paths) == 0 {
				return failureErrorf("no fixture files found in %s", strings.Join(args, ", "))
			}

			endpoint, err := capture.ResolveEndpoint(cfg.network(), cfg.rpcURL())
			if err != nil {
				return usageErrorf("%v", err)
			}
			src := newSource(cfg.network(), endpoint, cfg.timeout())

			cache := loadVerifyCache(cfg.verifyCachePath())

			var checked, matched, drifted, skipped, errored, filtered int
			for _, path := range paths {
				f, loadErr := golden.Load(path)
				if loadErr != nil {
					fmt.Fprintf(stderr, "%-40s ERROR   %v\n", path, loadErr)
					errored++
					checked++
					continue
				}

				// A fixture the filters exclude is not checked at all, and is
				// reported as filtered rather than quietly passing.
				if reason, excluded := excludedBy(filter, since, path, f); excluded {
					fmt.Fprintf(stdout, "%-40s FILTERED %s\n", path, reason)
					filtered++
					continue
				}

				// Under --drifted-only a fixture whose last check matched the
				// chain, and which has not changed since, is left alone.
				if cfg.driftedOnly() {
					entry, known := cache.lookup(path, f)
					if known && entry.Outcome == outcomeOK {
						fmt.Fprintf(stdout, "%-40s SKIP    last check on %s matched the chain\n",
							path, entry.CheckedAt)
						skipped++
						continue
					}
				}

				res := capture.Verify(cmd.Context(), src, path, f)
				switch {
				case res.Err != nil && errors.Is(res.Err, capture.ErrNotFound):
					fmt.Fprintf(stderr, "%-40s GONE    %v\n", path, res.Err)
					drifted++
					cache.record(path, f, outcomeDrifted)
				case res.Err != nil && (f.Captured == nil || f.Captured.TxHash == ""):
					fmt.Fprintf(stdout, "%-40s SKIP    %v\n", path, res.Err)
					skipped++
				case res.Err != nil:
					// The endpoint or the file failed, so this fixture was
					// not checked. That is not drift and must not be counted
					// as a pass.
					fmt.Fprintf(stderr, "%-40s ERROR   %v\n", path, res.Err)
					errored++
					cache.record(path, f, outcomeErrored)
				case res.Match:
					fmt.Fprintf(stdout, "%-40s ok\n", path)
					matched++
					cache.record(path, f, outcomeOK)
				default:
					fmt.Fprintf(stderr, "%-40s DRIFTED\n", path)
					if res.Diff != nil {
						f := lensformat.NewDiffFormatter(
							lensformat.WithDiffPalette(lensformat.PaletteFor(stderr, cfg.colorMode())))
						if fErr := f.Format(stderr, res.Diff); fErr != nil {
							fmt.Fprintf(stderr, "  (could not render the diff: %v)\n", fErr)
						}
					}
					drifted++
					cache.record(path, f, outcomeDrifted)
				}
				checked++
			}

			if err := cache.save(cfg.verifyCachePath()); err != nil {
				// The cache only saves work, so failing to write it is worth
				// saying but is not worth failing the run over.
				fmt.Fprintf(stderr, "warning: could not write the verification cache: %v\n", err)
			}

			fmt.Fprintf(stdout, "\n%d checked: %d ok, %d drifted, %d skipped",
				checked, matched, drifted, skipped)
			if errored > 0 {
				fmt.Fprintf(stdout, ", %d errored", errored)
			}
			if filtered > 0 {
				fmt.Fprintf(stdout, ", %d filtered", filtered)
			}
			fmt.Fprintln(stdout)

			if errored > 0 {
				return failureErrorf("%d fixture(s) could not be checked", errored)
			}
			if drifted > 0 {
				return driftErrorf("%d fixture(s) no longer match the chain", drifted)
			}
			return nil
		},
	}

	cfg.bindVerifyFlags(cmd)
	return cmd
}

// collectFixtures expands files and directories into a list of fixture paths.
func collectFixtures(args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", arg, err)
		}
		if !info.IsDir() {
			out = append(out, arg)
			continue
		}
		entries, err := filepath.Glob(filepath.Join(arg, "*.golden.json"))
		if err != nil {
			return nil, fmt.Errorf("scanning %s: %w", arg, err)
		}
		out = append(out, entries...)
	}
	return out, nil
}
