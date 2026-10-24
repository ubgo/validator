package validatorcmd

import (
	"bytes"
	"os"

	"github.com/spf13/cobra"

	"github.com/ubgo/validator/cli/outfmt"
	"github.com/ubgo/validator/i18n"
)

const flagCheck = "check"

// catalogReport is the --json payload of `catalog --check`.
type catalogReport struct {
	Locale  string   `json:"locale"`
	Codes   int      `json:"codes"`
	Missing []string `json:"missing,omitempty"`
}

func newCatalogCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Print the default message catalog, or check a translation for missing codes",
		Long: `Without flags, prints the built-in English catalog as JSON — the file a
translator starts from. With --check <file>, loads a translation and lists
every issue code it does not cover; exit 1 when any is missing, so a
partial translation fails CI rather than silently falling back to English.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			check, _ := cmd.Flags().GetString(flagCheck)
			if check == "" {
				doc, err := i18n.Default().JSON()
				if err != nil {
					_ = a.printer.Fail(outfmt.CodeInvalid, err)
					return failure(err)
				}
				if a.printer.JSON {
					return a.printer.OK(i18n.Default())
				}
				_, _ = a.printer.Out.Write(doc)
				return nil
			}
			data, err := os.ReadFile(check)
			if err != nil {
				_ = a.printer.Fail(outfmt.CodeIO, err)
				return failure(err)
			}
			cat, err := i18n.Load(bytes.NewReader(data))
			if err != nil {
				_ = a.printer.Fail(outfmt.CodeDecode, err)
				return failure(err)
			}
			report := catalogReport{Locale: cat.Locale, Codes: len(cat.Messages), Missing: cat.Missing()}
			a.printer.Humanf("%s: %d codes translated, %d missing", cat.Locale, report.Codes, len(report.Missing))
			for _, m := range report.Missing {
				a.printer.Humanf("  missing: %s", m)
			}
			if err := a.printer.OK(report); err != nil {
				return err
			}
			if len(report.Missing) > 0 {
				return failure(errIncomplete)
			}
			return nil
		},
	}
	cmd.Flags().String(flagCheck, "", "translation file to check against the default codes")
	return cmd
}

// errIncomplete is the failure a partial translation produces.
var errIncomplete = catalogError("catalog does not cover every code")

type catalogError string

func (e catalogError) Error() string { return string(e) }
