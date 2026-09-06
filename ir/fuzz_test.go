package ir

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The IR decoder is an untrusted-input surface: a schema document can come
// from a file, another language's emitter, or a request body. These fuzz
// targets pin the properties the rest of the library relies on, not merely
// that nothing panics.

// FuzzDecodeSchema checks the decoder's contract against arbitrary bytes:
//
//   - Decoding never panics; a malformed document is an error.
//   - A document that decodes AND passes Check re-marshals to something that
//     decodes and passes Check again. Without that, a document could be
//     accepted once and rejected after a round trip through a file or a
//     wire, which is exactly what "the serialized JSON is the stable
//     contract" (IR.md §9) forbids.
func FuzzDecodeSchema(f *testing.F) {
	golden, err := os.ReadFile(filepath.Join("testdata", "user.json"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(golden)
	f.Add([]byte(`{"version":"` + Version + `","root":{"kind":"string","rules":[{"kind":"minLength","params":{"n":3}}]}}`))
	f.Add([]byte(`{"version":"` + Version + `","root":{"kind":"object","fields":[{"name":"A","type":{"kind":"int"}}]}}`))
	f.Add([]byte(`{"version":"` + Version + `","root":{"kind":"oneof","discriminator":"k","cases":{"a":{"kind":"object"}}}}`))
	f.Add([]byte(`{"version":"x","root":{"kind":"nope"}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var s Schema
		if err := json.Unmarshal(data, &s); err != nil {
			return // a malformed document is a reported error, which is correct
		}
		if err := Check(&s); err != nil {
			return // structurally invalid: refused, also correct
		}
		// Accepted. It must survive a round trip unchanged in meaning.
		out, err := json.Marshal(&s)
		if err != nil {
			t.Fatalf("accepted schema does not marshal: %v", err)
		}
		var again Schema
		if err := json.Unmarshal(out, &again); err != nil {
			t.Fatalf("re-decode of an accepted schema failed: %v\n%s", err, out)
		}
		if err := Check(&again); err != nil {
			t.Fatalf("round trip made an accepted schema invalid: %v\n%s", err, out)
		}
		out2, err := json.Marshal(&again)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != string(out2) {
			t.Fatalf("re-marshal is not stable:\n%s\n%s", out, out2)
		}
	})
}

// FuzzDecodeType is the same contract one level down, where the kind
// discriminator is read. An unknown kind must be an error and never a
// silently skipped node — that invariant is what stops a rule from
// disappearing between two versions of the library.
func FuzzDecodeType(f *testing.F) {
	for _, seed := range []string{
		`{"kind":"string"}`,
		`{"kind":"array","items":{"kind":"int"}}`,
		`{"kind":"map","key":{"kind":"string"},"value":{"kind":"bool"}}`,
		`{"kind":"union","variants":[{"kind":"int"},{"kind":"string"}]}`,
		`{"kind":"ref","ref":"Address"}`,
		`{"kind":""}`,
		`{}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		n, err := DecodeType(data)
		if err != nil {
			return
		}
		if n == nil {
			t.Fatal("DecodeType returned a nil node and no error")
		}
		if n.Kind() == "" {
			t.Fatalf("decoded node has an empty kind: %#v", n)
		}
		out, err := json.Marshal(n)
		if err != nil {
			t.Fatalf("decoded node does not marshal: %v", err)
		}
		back, err := DecodeType(out)
		if err != nil {
			t.Fatalf("re-decode failed: %v\n%s", err, out)
		}
		if back.Kind() != n.Kind() {
			t.Fatalf("kind changed across a round trip: %s → %s", n.Kind(), back.Kind())
		}
	})
}
