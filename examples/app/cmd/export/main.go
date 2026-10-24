// Command export writes every artifact the rest of the application needs
// from the rules: the IR document, the JSON Schema the browser validates
// against, the manifest of what that schema could not carry, and the
// TypeScript module the frontend imports.
//
// Running it is the step that keeps the two sides in agreement. A team
// would run it from go:generate and commit the outputs, so a rule change
// that was not propagated shows up as a dirty diff in CI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ubgo/validator/examples/app/model"
	"github.com/ubgo/validator/jsonschema"
)

const filePerm = 0o644

func main() {
	dir := flag.String("dir", ".", "directory for booking.ir.json")
	web := flag.String("web", "../web", "directory for the frontend artifacts")
	flag.Parse()

	irDoc, err := json.MarshalIndent(model.Schema.IR(), "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	write(filepath.Join(*dir, "booking.ir.json"), append(irDoc, '\n'))

	res, err := jsonschema.Emit(model.Schema.IR(), jsonschema.WithID("https://example.com/schemas/booking.json"))
	if err != nil {
		log.Fatal(err)
	}
	schemaDoc, err := res.JSON()
	if err != nil {
		log.Fatal(err)
	}
	write(filepath.Join(*web, "booking.schema.json"), schemaDoc)

	ts, err := res.TSModule("booking")
	if err != nil {
		log.Fatal(err)
	}
	write(filepath.Join(*web, "booking.schema.ts"), ts)

	// The demo page is served as static files with no build step, so it
	// needs plain JavaScript. The .ts module is the same text with type
	// annotations; stripping "as const" and the type-only bits would be
	// fragile, so the JS is emitted directly from the same values.
	js, err := jsModule(res)
	if err != nil {
		log.Fatal(err)
	}
	write(filepath.Join(*web, "booking.schema.js"), js)

	// Say plainly what the browser will not be able to enforce. A team
	// reads this list once and knows exactly where the server is the only
	// line of defence.
	fmt.Println("\nrules the browser cannot run on its own:")
	for _, e := range res.Manifest.Entries {
		// ref matters: two custom rules on the same path are told apart
		// only by the function name they carry.
		where := e.Path
		if e.Ref != "" {
			where += " (" + e.Ref + ")"
		}
		fmt.Printf("  %-28s %-8s %-8s %s\n", where, e.Name, e.Class, e.Reason)
	}
}

func write(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, data, filePerm); err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote", path)
}

// jsModule renders the schema and manifest as an ES module a browser can
// import directly, no build step and no bundler.
func jsModule(res *jsonschema.Result) ([]byte, error) {
	schema, err := json.MarshalIndent(jsonschema.Inline(res.Schema), "", "  ")
	if err != nil {
		return nil, err
	}
	manifest, err := json.MarshalIndent(res.Manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("// Generated from the Go rules; do not edit.\n\n")
	b.WriteString("export const bookingSchema = ")
	b.Write(schema)
	b.WriteString(";\n\nexport const bookingManifest = ")
	b.Write(manifest)
	b.WriteString(";\n")
	return []byte(b.String()), nil
}
