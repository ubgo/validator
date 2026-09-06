package example

import (
	"encoding/json"
	"os"
	"testing"
)

// TestBuilderSourceRoundTrips is the migration's proof: the emitted
// schema.gen.go compiled (or this test would not exist), and building it
// yields exactly the IR the migration produced from the tags.
func TestBuilderSourceRoundTrips(t *testing.T) {
	want, err := os.ReadFile("signup.ir.json")
	if err != nil {
		t.Fatalf("signup.ir.json missing; run UPDATE_GOLDEN=1 go test ./tags: %v", err)
	}
	var wantDoc map[string]any
	if err := json.Unmarshal(want, &wantDoc); err != nil {
		t.Fatal(err)
	}
	// The migration records its generator name; the builder cannot, so
	// compare everything but meta.generator.
	delete(wantDoc["meta"].(map[string]any), "generator")
	got, err := json.Marshal(SignupSchema.IR())
	if err != nil {
		t.Fatal(err)
	}
	var gotDoc map[string]any
	if err := json.Unmarshal(got, &gotDoc); err != nil {
		t.Fatal(err)
	}
	wantJSON, _ := json.Marshal(wantDoc)
	gotJSON, _ := json.Marshal(gotDoc)
	if string(wantJSON) != string(gotJSON) {
		t.Fatalf("builder source does not rebuild the migrated IR\nwant: %s\ngot:  %s", wantJSON, gotJSON)
	}
	if AddressSchema.Name() != "Address" {
		t.Error("nested schema")
	}
	if (Signup{unexport: "x"}).HasUnexported() != true || (Signup{}).HasUnexported() {
		t.Error("HasUnexported")
	}
}
