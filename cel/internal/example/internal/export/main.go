// Command export writes the IR document `validatorctl gen` reads.
package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/ubgo/validator/cel/internal/example"
)

func main() {
	doc, err := json.MarshalIndent(example.Schema.IR(), "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("stay.ir.json", append(doc, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}
