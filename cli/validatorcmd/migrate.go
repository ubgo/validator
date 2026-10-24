package validatorcmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ubgo/validator/cli/outfmt"
	"github.com/ubgo/validator/tags"
)

// Flags for `migrate`.
const (
	flagBuilder = "builder"
	flagPartial = "allow-partial"
)

// migrateReport is the --json payload of `migrate`.
type migrateReport struct {
	Dir         string   `json:"dir"`
	Type        string   `json:"type"`
	Types       []string `json:"types"`
	IR          string   `json:"ir,omitempty"`
	Builder     string   `json:"builder,omitempty"`
	Unsupported []string `json:"unsupported,omitempty"`
	Lossless    bool     `json:"lossless"`
}

func newMigrateCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate [dir]",
		Short: "Convert struct validate tags into an IR document and builder source",
		Long: `Reads the validate:"..." tags of --type (and every struct it references) and
writes the equivalent IR document (--out) and, optionally, vb builder source
(--builder) that uses the <Type>Fields accessors from 'validatorctl fields'.

Every tag the migration cannot express is listed. The command fails on any
such tag unless --allow-partial is given, so a lossy migration is a decision,
never an accident.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			typ, _ := cmd.Flags().GetString(flagType)
			out, _ := cmd.Flags().GetString(flagOut)
			builder, _ := cmd.Flags().GetString(flagBuilder)
			partial, _ := cmd.Flags().GetBool(flagPartial)
			suffix, _ := cmd.Flags().GetString(flagSuffix)
			dry, _ := cmd.Flags().GetBool(flagDryRun)

			res, err := tags.Migrate(tags.Options{Dir: dir, Type: typ, Generator: "validatorctl migrate"})
			if err != nil {
				_ = a.printer.Fail(outfmt.CodeInvalid, err)
				return failure(err)
			}
			report := migrateReport{Dir: dir, Type: typ, Types: res.Types, Lossless: len(res.Unsupported) == 0}
			for _, u := range res.Unsupported {
				report.Unsupported = append(report.Unsupported, u.String())
			}
			irDoc, err := json.MarshalIndent(res.Schema, "", "  ")
			if err != nil {
				_ = a.printer.Fail(outfmt.CodeInvalid, err)
				return failure(err)
			}
			irDoc = append(irDoc, '\n')
			var builderSrc []byte
			if builder != "" {
				pkgName, err := packageName(dir)
				if err != nil {
					_ = a.printer.Fail(outfmt.CodeIO, err)
					return failure(err)
				}
				builderSrc, err = tags.Builder(res.Schema, res.Types, tags.BuilderOptions{Package: pkgName, Suffix: suffix})
				if err != nil {
					_ = a.printer.Fail(outfmt.CodeInvalid, err)
					return failure(err)
				}
			}
			if dry {
				_, _ = a.printer.Out.Write(irDoc)
				if builderSrc != nil {
					_, _ = a.printer.Out.Write(builderSrc)
				}
			} else {
				if out != "" {
					if err := os.WriteFile(out, irDoc, 0o644); err != nil {
						_ = a.printer.Fail(outfmt.CodeIO, err)
						return failure(err)
					}
					report.IR = out
					a.printer.Humanf("wrote %s", out)
				}
				if builderSrc != nil {
					path := filepath.Join(dir, builder)
					if err := os.WriteFile(path, builderSrc, 0o644); err != nil {
						_ = a.printer.Fail(outfmt.CodeIO, err)
						return failure(err)
					}
					report.Builder = path
					a.printer.Humanf("wrote %s", path)
				}
			}
			a.printer.Humanf("migrated %s", strings.Join(res.Types, ", "))
			for _, u := range report.Unsupported {
				a.printer.Humanf("  unsupported: %s", u)
			}
			if err := a.printer.OK(report); err != nil {
				return err
			}
			if !report.Lossless && !partial {
				return failure(errors.New("migration is not lossless; pass --allow-partial to accept"))
			}
			return nil
		},
	}
	cmd.Flags().String(flagType, "", "root struct to migrate (required)")
	_ = cmd.MarkFlagRequired(flagType)
	cmd.Flags().String(flagOut, "", "write the IR document to this file")
	cmd.Flags().String(flagBuilder, "", "also write vb builder source to this file name inside the package directory")
	cmd.Flags().String(flagSuffix, "Fields", "accessor variable suffix used by the builder source")
	cmd.Flags().Bool(flagPartial, false, "exit 0 even when some tags could not be migrated")
	cmd.Flags().Bool(flagDryRun, false, "print the IR (and builder source) instead of writing")
	return cmd
}

// packageName reads the package clause of the first Go file in dir.
func packageName(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return "", err
		}
		for _, line := range strings.Split(string(data), "\n") {
			if name, ok := strings.CutPrefix(strings.TrimSpace(line), "package "); ok {
				return strings.TrimSpace(name), nil
			}
		}
	}
	return "", errors.New("no Go package in " + dir)
}
