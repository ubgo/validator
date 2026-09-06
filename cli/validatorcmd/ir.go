package validatorcmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ubgo/validator/cli/outfmt"
	"github.com/ubgo/validator/ir"
)

// checkReport is the data payload of `ir check`. Problems is a flat list so
// a CI script can print every finding at once.
type checkReport struct {
	File     string   `json:"file"`
	Version  string   `json:"version"`
	Valid    bool     `json:"valid"`
	Problems []string `json:"problems,omitempty"`
}

func newIRCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ir",
		Short: "Work with serialized IR documents",
	}
	cmd.AddCommand(newIRCheckCmd(a), newIRExportCmd(a))
	return cmd
}

func newIRCheckCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "check <file.json>",
		Short: "Decode a serialized IR document and run the structural Check",
		Long: `Reads a serialized IR document, decodes it, and reports every structural
problem: unknown kinds, unresolved refs, missing rule params, portability
claims wider than the rule kind allows. Exit 1 when the document is invalid.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path := args[0]
			data, err := os.ReadFile(path)
			if err != nil {
				_ = a.printer.Fail(outfmt.CodeIO, err)
				return failure(err)
			}
			var s ir.Schema
			if err := json.Unmarshal(data, &s); err != nil {
				_ = a.printer.Fail(outfmt.CodeDecode, err)
				return failure(err)
			}
			report := checkReport{File: path, Version: s.Version, Valid: true}
			if err := ir.Check(&s); err != nil {
				report.Valid = false
				report.Problems = flatten(err)
			}
			if report.Valid {
				a.printer.Humanf("%s: ok (%s)", path, s.Version)
				return a.printer.OK(report)
			}
			a.printer.Humanf("%s: %d problem(s)", path, len(report.Problems))
			for _, p := range report.Problems {
				a.printer.Humanf("  - %s", p)
			}
			// In JSON mode the report itself is the envelope's data; the
			// exit code carries the verdict.
			_ = a.printer.OK(report)
			return failure(fmt.Errorf("%s: invalid IR", path))
		},
	}
}

// flatten expands an errors.Join tree into its leaf messages so each
// finding is one line.
func flatten(err error) []string {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var out []string
		for _, e := range joined.Unwrap() {
			out = append(out, flatten(e)...)
		}
		return out
	}
	return []string{err.Error()}
}
