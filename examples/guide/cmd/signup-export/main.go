// Command signup-export writes the artifacts the rest of the toolchain
// consumes from signup.Schema: the IR document (input to `validatorctl gen`
// and `validatorctl ir export`), the JSON Schema, and the TypeScript module
// the frontend imports. Run it from a go:generate line or a Taskfile and
// commit the outputs; CI fails when they are stale.
//
//	go run ./guide/cmd/signup-export -dir ./web/src
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ubgo/validator/examples/guide/signup"
	"github.com/ubgo/validator/jsonschema"
)

// filePerm is the mode of every generated file: readable by everyone,
// writable by the owner, never executable.
const filePerm = 0o644

func main() {
	dir := flag.String("dir", ".", "directory to write signup.ir.json, signup.schema.json and signup.schema.ts into")
	flag.Parse()

	// 1. The IR: the rules as data. This is the single source of truth every
	//    other artifact derives from.
	irDoc, err := json.MarshalIndent(signup.Schema.IR(), "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	write(*dir, "signup.ir.json", irDoc)

	// 2. JSON Schema + manifest. Emit refuses an IR that fails ir.Check.
	res, err := jsonschema.Emit(signup.Schema.IR(), jsonschema.WithID("https://example.com/schemas/signup.json"))
	if err != nil {
		log.Fatal(err)
	}
	schemaDoc, err := res.JSON()
	if err != nil {
		log.Fatal(err)
	}
	write(*dir, "signup.schema.json", schemaDoc)

	// 3. The TypeScript module: schema and manifest as `as const` literals,
	//    which is what lets the runtime infer the value type.
	tsDoc, err := res.TSModule("signup")
	if err != nil {
		log.Fatal(err)
	}
	write(*dir, "signup.schema.ts", tsDoc)

	// 4. Say what the browser will NOT enforce. Never empty for this schema:
	//    the custom rule, the async rule, the cross-field After, and the two
	//    transforms all land here.
	for _, e := range res.Manifest.Entries {
		fmt.Printf("%-12s %-10s %-9s %s: %s\n", e.Path, e.Name, e.Class, e.Kind, e.Reason)
	}
}

func write(dir, name string, data []byte) {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, filePerm); err != nil {
		log.Fatal(err)
	}
	fmt.Println("wrote", path)
}
