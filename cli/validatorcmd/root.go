// Package validatorcmd wires the validatorctl verb tree.
//
// Design rules (mirrors ubgo/dotenv's dotenvcmd — keep them or the commands
// stop being embeddable):
//
//   - constructor-per-command (newXCmd(app)): no init() registration and no
//     package-level state, so a host application can mount the tree twice or
//     under another root without cross-talk;
//   - all output flows through the app's outfmt.Printer — a verb never prints
//     to os.Stdout directly, which is what makes tests and --json airtight.
package validatorcmd

import (
	"errors"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/ubgo/validator/cli/outfmt"
)

// Exit codes — the CLI's contract with shells and CI. Closed set; scripts
// branch on these, so the values are frozen API.
const (
	// ExitOK: the operation succeeded.
	ExitOK = 0

	// ExitFailure: the operation ran and failed (an IR document did not
	// pass Check, a file was unreadable).
	ExitFailure = 1

	// ExitUsage: the invocation itself was malformed (unknown flag, missing
	// operand). Mirrors the BSD EX_USAGE convention that grep/diff follow.
	ExitUsage = 2
)

// Flag names shared across verbs — defined once so a rename cannot leave a
// stale literal behind in one verb's lookup.
const (
	flagJSON   = "json"
	flagDryRun = "dry-run"
)

// devVersion is reported when the binary was built outside a module (no
// VCS info), e.g. `go run` from the working tree.
const devVersion = "dev"

// exitError carries an exit code through cobra's error return so Execute
// can map operation failures (1) apart from usage errors (2) without
// stringly inspection. Every RunE failure a verb REPORTS deliberately is one
// of these; anything else bubbling out of cobra is by definition a usage
// problem.
type exitError struct {
	code int
	// quiet suppresses Execute's fallback printing — set when the verb
	// already emitted its own failure envelope via the Printer.
	quiet bool
	err   error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// failure wraps an operation error the verb has already printed.
func failure(err error) error { return &exitError{code: ExitFailure, quiet: true, err: err} }

// app is the per-invocation state every verb reads. One instance per
// Execute call; never package-level.
type app struct {
	printer outfmt.Printer
	stderr  io.Writer
}

// Execute runs the CLI with the given arguments and writers and returns the
// process exit code. It is the single entry point for both main and tests.
func Execute(args []string, stdout, stderr io.Writer) int {
	a := &app{printer: outfmt.Printer{Out: stdout}, stderr: stderr}
	root := newRootCmd(a)
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	err := root.Execute()
	if err == nil {
		return ExitOK
	}
	// errors.As rather than errors.AsType: the module floor is Go 1.24 and
	// AsType arrived in 1.26.
	var ee *exitError
	if errors.As(err, &ee) {
		if !ee.quiet {
			_, _ = fmt.Fprintf(stderr, "error: %v\n", ee.err)
		}
		return ee.code
	}
	// cobra already printed usage errors to stderr.
	return ExitUsage
}

func newRootCmd(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "validatorctl",
		Short: "Inspect and check ubgo/validator IR documents",
		Long: `validatorctl works with the serializable rule-tree IR of github.com/ubgo/validator.

Every verb supports --json for a stable machine-readable envelope.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			// Resolve the output mode once, here, so verbs never consult flags.
			asJSON, _ := cmd.Flags().GetBool(flagJSON)
			a.printer.JSON = asJSON
		},
	}
	root.PersistentFlags().Bool(flagJSON, false, "emit a machine-readable JSON envelope")
	root.AddCommand(newVersionCmd(a), newIRCmd(a), newFieldsCmd(a), newGenCmd(a), newMigrateCmd(a), newCatalogCmd(a))
	return root
}

// versionInfo is the data payload of the version verb.
type versionInfo struct {
	Version string `json:"version"`
	Go      string `json:"go"`
}

func newVersionCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the validatorctl version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			v := versionInfo{Version: devVersion}
			if bi, ok := debug.ReadBuildInfo(); ok {
				v.Go = bi.GoVersion
				if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
					v.Version = bi.Main.Version
				}
			}
			a.printer.Humanf("validatorctl %s (%s)", v.Version, v.Go)
			return a.printer.OK(v)
		},
	}
}
