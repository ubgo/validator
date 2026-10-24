package validatorcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ubgo/validator/cli/outfmt"
	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/jsonschema"
)

// Flags for `ir export`.
const (
	flagDraft    = "draft"
	flagSchemaID = "id"
	flagOut      = "out"
	flagManifest = "manifest"
	flagTS       = "ts"
	flagName     = "name"
)

// Draft flag values. Closed set; the strings are what users type, so they
// are frozen API.
const (
	draft2020 = "2020"
	draft07   = "07"
)

// exportReport is the --json payload: the schema document and the manifest
// together, so a CI step can gate on the manifest in one call.
type exportReport struct {
	File     string              `json:"file"`
	Draft    string              `json:"draft"`
	Out      string              `json:"out,omitempty"`
	TS       string              `json:"ts,omitempty"`
	Schema   *jsonschema.Object  `json:"schema"`
	Manifest jsonschema.Manifest `json:"manifest"`
	Portable bool                `json:"portable"`
}

func newIRExportCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <file.json>",
		Short: "Emit JSON Schema (and the portability manifest) from a serialized IR document",
		Long: `Reads a serialized IR document and emits JSON Schema. Non-portable rules
(custom functions, async rules, cross-field comparisons, transforms) are
listed in the portability manifest, never silently dropped.

Human mode prints the schema to stdout (or writes --out) and the manifest
summary to stderr. --json wraps schema and manifest in one envelope.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			draftFlag, _ := cmd.Flags().GetString(flagDraft)
			id, _ := cmd.Flags().GetString(flagSchemaID)
			outPath, _ := cmd.Flags().GetString(flagOut)
			showManifest, _ := cmd.Flags().GetBool(flagManifest)
			tsPath, _ := cmd.Flags().GetString(flagTS)
			name, _ := cmd.Flags().GetString(flagName)

			var draft jsonschema.Draft
			switch draftFlag {
			case draft2020:
				draft = jsonschema.Draft2020
			case draft07:
				draft = jsonschema.Draft07
			default:
				err := fmt.Errorf("unknown --draft %q (want %s or %s)", draftFlag, draft2020, draft07)
				_ = a.printer.Fail(outfmt.CodeUsage, err)
				return failure(err)
			}

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
			res, err := jsonschema.Emit(&s, jsonschema.WithDraft(draft), jsonschema.WithID(id))
			if err != nil {
				_ = a.printer.Fail(outfmt.CodeInvalid, err)
				return failure(err)
			}
			doc, err := res.JSON()
			if err != nil {
				_ = a.printer.Fail(outfmt.CodeInvalid, err)
				return failure(err)
			}

			report := exportReport{File: path, Draft: draftFlag, Out: outPath, TS: tsPath, Schema: res.Schema, Manifest: res.Manifest, Portable: res.Manifest.Portable()}
			if tsPath != "" {
				if name == "" {
					name = tsName(s.Meta.Title)
				}
				mod, err := res.TSModule(name)
				if err != nil {
					_ = a.printer.Fail(outfmt.CodeInvalid, err)
					return failure(err)
				}
				if err := os.WriteFile(tsPath, mod, 0o644); err != nil {
					_ = a.printer.Fail(outfmt.CodeIO, err)
					return failure(err)
				}
				a.printer.Humanf("wrote %s (%sSchema, %sManifest)", tsPath, name, name)
			}
			if outPath != "" {
				if err := os.WriteFile(outPath, doc, 0o644); err != nil {
					_ = a.printer.Fail(outfmt.CodeIO, err)
					return failure(err)
				}
				a.printer.Humanf("wrote %s (%d bytes)", outPath, len(doc))
			} else if !a.printer.JSON {
				_, _ = a.printer.Out.Write(doc)
			}
			if !a.printer.JSON && (showManifest || !res.Manifest.Portable()) {
				_, _ = fmt.Fprintf(a.stderr, "portability manifest: %d non-portable element(s)\n", res.Manifest.Len())
				for _, e := range res.Manifest.Entries {
					_, _ = fmt.Fprintf(a.stderr, "  - %s %s %s [%s] %s\n", e.Path, e.Kind, e.Name, e.Class, e.Reason)
				}
			}
			return a.printer.OK(report)
		},
	}
	cmd.Flags().String(flagDraft, draft2020, "JSON Schema dialect: 2020 or 07")
	cmd.Flags().String(flagSchemaID, "", "value for $id")
	cmd.Flags().String(flagOut, "", "write the schema to this file instead of stdout")
	cmd.Flags().Bool(flagManifest, false, "always print the manifest summary, even when empty")
	cmd.Flags().String(flagTS, "", "also write a TypeScript module exporting <name>Schema and <name>Manifest as const")
	cmd.Flags().String(flagName, "", "identifier prefix for --ts (default: the schema title, lower-cased first letter)")
	return cmd
}

// tsName derives the TS identifier prefix from the schema title:
// "User" → "user"; an empty title falls back to "schema".
func tsName(title string) string {
	if title == "" {
		return "schema"
	}
	return strings.ToLower(title[:1]) + title[1:]
}
