package validatorcmd

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ubgo/validator/cli/outfmt"
	"github.com/ubgo/validator/codegen"
	"github.com/ubgo/validator/ir"
)

const flagSchema = "schema"

// genReport is the --json payload of `gen`.
type genReport struct {
	Dir     string   `json:"dir"`
	Schema  string   `json:"schema"`
	Path    string   `json:"path"`
	Funcs   []string `json:"funcs"`
	Written bool     `json:"written"`
}

func newGenCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen [dir]",
		Short: "Generate specialized Validate<Type> functions from a serialized IR document",
		Long: `Reads an IR document (--schema) whose root title and defs name structs in
the package directory, and writes validate.gen.go with one reflection-free
Validate<Type>(ctx, *Type, rt.Registry) function per struct, plus a
Validate<Type>Into variant that writes into a caller-owned issue list.
Unions, discriminated unions and transforms compile to Go; the parts the
generator cannot type statically (a discriminated union on an interface
field, a union with collection variants) run the interpreter on that
subtree at the same path. CEL conditions are refused.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			schemaPath, _ := cmd.Flags().GetString(flagSchema)
			out, _ := cmd.Flags().GetString(flagOut)
			dry, _ := cmd.Flags().GetBool(flagDryRun)
			data, err := os.ReadFile(schemaPath)
			if err != nil {
				_ = a.printer.Fail(outfmt.CodeIO, err)
				return failure(err)
			}
			var s ir.Schema
			if err := json.Unmarshal(data, &s); err != nil {
				_ = a.printer.Fail(outfmt.CodeDecode, err)
				return failure(err)
			}
			opts := codegen.Options{Schema: &s, Dir: dir, Output: out}
			var res *codegen.Result
			if dry {
				res, err = codegen.Generate(opts)
			} else {
				res, err = codegen.Write(opts)
			}
			if err != nil {
				_ = a.printer.Fail(outfmt.CodeInvalid, err)
				return failure(err)
			}
			if dry {
				_, _ = a.printer.Out.Write(res.Source)
			} else {
				a.printer.Humanf("wrote %s (%s)", res.Path, strings.Join(res.Funcs, ", "))
			}
			return a.printer.OK(genReport{Dir: dir, Schema: schemaPath, Path: res.Path, Funcs: res.Funcs, Written: !dry})
		},
	}
	cmd.Flags().String(flagSchema, "", "serialized IR document (required)")
	_ = cmd.MarkFlagRequired(flagSchema)
	cmd.Flags().String(flagOut, codegen.DefaultOutput, "output file name inside the package directory")
	cmd.Flags().Bool(flagDryRun, false, "print the generated source instead of writing it")
	return cmd
}
