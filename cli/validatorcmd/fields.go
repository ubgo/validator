package validatorcmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/ubgo/validator/accessors"
	"github.com/ubgo/validator/cli/outfmt"
)

// Flags for `fields`.
const (
	flagType       = "type"
	flagSuffix     = "suffix"
	flagUnexported = "unexported"
)

// fieldsReport is the --json payload of `fields`.
type fieldsReport struct {
	Dir     string   `json:"dir"`
	Path    string   `json:"path"`
	Types   []string `json:"types"`
	Skipped []string `json:"skipped,omitempty"`
	Written bool     `json:"written"`
}

func newFieldsCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fields [dir]",
		Short: "Generate typed field accessors (<Type>Fields) for the structs in a package",
		Long: `Scans a Go package directory and writes fields.gen.go containing a
<Type>Fields value per struct: one vb.FieldRef per exported field, carrying
the Go name, json wire name and getter. Author rules against these instead
of string field names; re-running never touches your hand-written rules.

Add to the package:  //go:generate validatorctl fields
Dry run:             validatorctl fields --dry-run`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			types, _ := cmd.Flags().GetStringSlice(flagType)
			out, _ := cmd.Flags().GetString(flagOut)
			suffix, _ := cmd.Flags().GetString(flagSuffix)
			unexported, _ := cmd.Flags().GetBool(flagUnexported)
			dry, _ := cmd.Flags().GetBool(flagDryRun)

			opts := accessors.Options{Dir: dir, Types: types, Output: out, Suffix: suffix, IncludeUnexported: unexported}
			var (
				res *accessors.Result
				err error
			)
			if dry {
				res, err = accessors.Generate(opts)
			} else {
				res, err = accessors.Write(opts)
			}
			if err != nil {
				_ = a.printer.Fail(outfmt.CodeInvalid, err)
				return failure(err)
			}
			report := fieldsReport{Dir: dir, Path: res.Path, Types: res.Types, Skipped: res.Skipped, Written: !dry}
			if dry {
				_, _ = a.printer.Out.Write(res.Source)
			} else {
				a.printer.Humanf("wrote %s (%s)", res.Path, strings.Join(res.Types, ", "))
			}
			for _, s := range res.Skipped {
				a.printer.Humanf("  skipped: %s", s)
			}
			return a.printer.OK(report)
		},
	}
	cmd.Flags().StringSlice(flagType, nil, "only these struct names (repeatable)")
	cmd.Flags().String(flagOut, accessors.DefaultOutput, "output file name inside the package directory")
	cmd.Flags().String(flagSuffix, accessors.DefaultSuffix, "suffix for the accessor variable name")
	cmd.Flags().Bool(flagUnexported, false, "also generate accessors for unexported fields")
	cmd.Flags().Bool(flagDryRun, false, "print the generated source instead of writing it")
	return cmd
}
