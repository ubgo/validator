package examples

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/ir"
	vjs "github.com/ubgo/validator/jsonschema"
	"github.com/ubgo/validator/rulehttp"
	"github.com/ubgo/validator/vb"
)

// This file is the two-sided proof of the round-trip contract: the REAL
// TypeScript runtime (ts/dist, built by `task ts:build`) runs under Node
// against a REAL rulehttp server, using the schema and manifest emitted by
// the Go side. Neither side is mocked. Any drift in the wire format —
// request body, response shape, endpoint, issue fields — fails here.

type Account struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

var (
	fEmail = vb.NewField("Email", func(a Account) string { return a.Email }).JSON("email")
	fName  = vb.NewField("Name", func(a Account) string { return a.Name }).JSON("name")
)

func accountSchema() *vb.Schema[Account] {
	return vb.Struct("Account",
		vb.F(fEmail, vb.String().Email().Async("unique_email", "/v/unique_email", func(_ context.Context, s string) error {
			if s == "taken@example.com" {
				return errors.New("already taken")
			}
			return nil
		})),
		vb.F(fName, vb.String().MinLength(2)),
	).MustBuild()
}

// distIndex locates the built TypeScript runtime. Its absence is a hard
// failure, not a skip: the root gate builds ts before running this module
// so a missing dist means the pipeline is broken.
func distIndex(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "ts", "dist", "index.js"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("ts/dist/index.js missing — run `task ts:build` first: %v", err)
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatalf("node is required for the round-trip proof: %v", err)
	}
	return p
}

// nodeResult is what the Node script prints.
type nodeResult struct {
	Local  json.RawMessage `json:"local"`
	Remote json.RawMessage `json:"remote"`
	Calls  []struct {
		URL  string          `json:"url"`
		Body json.RawMessage `json:"body"`
	} `json:"calls"`
}

// runNode validates value through the real runtime, returning the local
// and server-inclusive results plus every HTTP call the runtime made.
func runNode(t *testing.T, dist, baseURL string, schema, manifest, value []byte) nodeResult {
	t.Helper()
	script := `
const { createValidator } = await import(process.argv[1]);
const [schema, manifest, value, baseURL] = [JSON.parse(process.argv[2]), JSON.parse(process.argv[3]), JSON.parse(process.argv[4]), process.argv[5]];
const calls = [];
const realFetch = globalThis.fetch;
const v = createValidator(schema, {
  manifest, baseURL, debounceMs: 1,
  onNonPortable: () => {},
  fetch: async (url, init) => { calls.push({ url: String(url), body: JSON.parse(String(init.body)) }); return realFetch(url, init); },
});
const local = v.validate(value);
const remote = await v.validateAsync(value);
console.log(JSON.stringify({ local, remote, calls }));
`
	cmd := exec.Command("node", "--input-type=module", "-e", script, dist, string(schema), string(manifest), string(value), baseURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node failed: %v\n%s", err, out)
	}
	var res nodeResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &res); err != nil {
		t.Fatalf("node output %q: %v", out, err)
	}
	return res
}

func TestRoundTripThroughRealRuntime(t *testing.T) {
	dist := distIndex(t)
	schema := accountSchema()

	// Go side: emit schema + manifest, serve the async rule.
	res, err := vjs.Emit(schema.IR())
	if err != nil {
		t.Fatal(err)
	}
	schemaDoc, _ := res.JSON()
	manifestDoc, _ := json.Marshal(res.Manifest)
	if len(res.Manifest.Entries) != 1 || res.Manifest.Entries[0].Class != vjs.ClassServer {
		t.Fatalf("manifest %+v", res.Manifest)
	}
	h, err := rulehttp.FromSchema(schema, rulehttp.WithLimiter(rulehttp.NoLimit))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Valid locally and remotely.
	free, _ := json.Marshal(Account{Email: "free@example.com", Name: "Ada"})
	r := runNode(t, dist, srv.URL, schemaDoc, manifestDoc, free)
	if !strings.Contains(string(r.Local), `"ok":true`) || !strings.Contains(string(r.Remote), `"ok":true`) {
		t.Fatalf("free account: local=%s remote=%s", r.Local, r.Remote)
	}
	if len(r.Calls) != 1 || r.Calls[0].URL != srv.URL+"/v/unique_email" || string(r.Calls[0].Body) != `{"rule":"unique_email","value":"free@example.com"}` {
		t.Fatalf("wire request: %+v", r.Calls)
	}

	// Valid locally, rejected by the server rule: the runtime merges the
	// server's issue with the Go code/message.
	taken, _ := json.Marshal(Account{Email: "taken@example.com", Name: "Ada"})
	r = runNode(t, dist, srv.URL, schemaDoc, manifestDoc, taken)
	if !strings.Contains(string(r.Local), `"ok":true`) {
		t.Fatalf("taken should pass locally: %s", r.Local)
	}
	var remote struct {
		OK     bool `json:"ok"`
		Issues []struct {
			Code    string `json:"code"`
			Path    string `json:"path"`
			Message string `json:"message"`
			Rule    string `json:"rule"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(r.Remote, &remote); err != nil {
		t.Fatal(err)
	}
	if remote.OK || len(remote.Issues) != 1 || remote.Issues[0].Code != "custom" || remote.Issues[0].Message != "already taken" || remote.Issues[0].Path != "$.email" || remote.Issues[0].Rule != "async" {
		t.Fatalf("server issue not merged as expected: %s", r.Remote)
	}

	// Invalid locally: no server call is made at all.
	bad, _ := json.Marshal(Account{Email: "nope", Name: "A"})
	r = runNode(t, dist, srv.URL, schemaDoc, manifestDoc, bad)
	if !strings.Contains(string(r.Local), `"code":"email"`) || !strings.Contains(string(r.Local), `"code":"min_length"`) || len(r.Calls) != 0 {
		t.Fatalf("local failure must short-circuit: local=%s calls=%+v", r.Local, r.Calls)
	}

	// The Go side agrees with the browser on the same inputs.
	for _, c := range []struct {
		in   Account
		want bool
	}{{Account{Email: "free@example.com", Name: "Ada"}, true}, {Account{Email: "taken@example.com", Name: "Ada"}, false}, {Account{Email: "nope", Name: "A"}, false}} {
		v := mustInterp(t, schema)
		if got := v.Validate(context.Background(), c.in) == nil; got != c.want {
			t.Errorf("Go verdict for %+v = %v want %v", c.in, got, c.want)
		}
	}
	_ = ir.Version
}

// mustInterp compiles the schema's interpreter for the Go-side verdicts.
func mustInterp(t *testing.T, s *vb.Schema[Account]) *interp.Validator {
	t.Helper()
	v, err := interp.FromSchema(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
