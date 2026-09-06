// Command export writes benchmodel's IR document, the input to
// `validatorctl gen`. Run through go:generate in the parent package.
package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/ubgo/validator/examples/benchmodel"
)

func main() {
	doc, err := json.MarshalIndent(benchmodel.Schema.IR(), "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("bench.ir.json", append(doc, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}
