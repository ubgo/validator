package validatorcmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the CLI in-process and returns exit code, stdout, stderr.
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Execute(args, &out, &errb)
	return code, out.String(), errb.String()
}

// goldenIR is the library's fixture — the CLI test reuses it so the two
// modules cannot disagree about what a valid document looks like.
func goldenIR(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "ir", "testdata", "user.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("golden fixture missing: %v", err)
	}
	return p
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ir.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestVersion(t *testing.T) {
	code, out, _ := run(t, "version")
	if code != ExitOK || !strings.HasPrefix(out, "validatorctl ") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = run(t, "version", "--json")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	var env struct {
		OK   bool        `json:"ok"`
		Data versionInfo `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || !env.OK || env.Data.Version == "" {
		t.Fatalf("bad envelope %q: %v", out, err)
	}
}

func TestIRCheckValid(t *testing.T) {
	p := goldenIR(t)
	code, out, _ := run(t, "ir", "check", p)
	if code != ExitOK || !strings.Contains(out, ": ok (") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = run(t, "ir", "check", p, "--json")
	if code != ExitOK {
		t.Fatalf("code=%d out=%q", code, out)
	}
	var env struct {
		OK   bool        `json:"ok"`
		Data checkReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK || !env.Data.Valid || len(env.Data.Problems) != 0 || env.Data.File != p {
		t.Fatalf("bad report: %+v", env)
	}
}

func TestIRCheckInvalid(t *testing.T) {
	// Decodes but fails Check on two counts: wrong version and an
	// unresolved ref. Both must be listed.
	p := writeTemp(t, `{"version":"v0","root":{"kind":"ref","ref":"Nope"}}`)
	code, out, _ := run(t, "ir", "check", p)
	if code != ExitFailure {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "2 problem(s)") || !strings.Contains(out, "version") || !strings.Contains(out, "Nope") {
		t.Fatalf("out=%q", out)
	}
	code, out, _ = run(t, "ir", "check", p, "--json")
	if code != ExitFailure {
		t.Fatalf("code=%d", code)
	}
	var env struct {
		OK   bool        `json:"ok"`
		Data checkReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Valid || len(env.Data.Problems) != 2 {
		t.Fatalf("bad report: %+v", env)
	}
}

func TestIRCheckDecodeError(t *testing.T) {
	p := writeTemp(t, `{"version":"x","root":{"kind":"nope"}}`)
	code, out, _ := run(t, "ir", "check", p, "--json")
	if code != ExitFailure || !strings.Contains(out, `"code":"decode"`) {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = run(t, "ir", "check", p)
	if code != ExitFailure || !strings.HasPrefix(out, "error: ") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestIRCheckMissingFile(t *testing.T) {
	code, out, _ := run(t, "ir", "check", filepath.Join(t.TempDir(), "missing.json"), "--json")
	if code != ExitFailure || !strings.Contains(out, `"code":"io"`) {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestUsageErrors(t *testing.T) {
	if code, _, _ := run(t, "ir", "check"); code != ExitUsage {
		t.Errorf("missing operand: code=%d", code)
	}
	if code, _, _ := run(t, "--bogus"); code != ExitUsage {
		t.Errorf("unknown flag: code=%d", code)
	}
	if code, _, _ := run(t, "version", "extra"); code != ExitUsage {
		t.Errorf("extra arg: code=%d", code)
	}
}

func TestFlatten(t *testing.T) {
	if got := flatten(nil); got != nil {
		t.Errorf("nil → %v", got)
	}
	code, out, _ := run(t, "ir", "check", writeTemp(t, `{"version":"v0","root":{"kind":"any"}}`), "--json")
	if code != ExitFailure || !strings.Contains(out, `"problems":["ir: version`) {
		t.Fatalf("single finding must still be a one-element list: %q", out)
	}
}

func TestIRExport(t *testing.T) {
	p := goldenIR(t)
	// Human mode: schema on stdout, manifest summary on stderr.
	code, out, errOut := run(t, "ir", "export", p)
	if code != ExitOK || !strings.HasPrefix(out, "{\n  \"$schema\": \"https://json-schema.org/draft/2020-12/schema\"") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(errOut, "portability manifest: 6 non-portable") || !strings.Contains(errOut, "$.email rule async [server]") {
		t.Fatalf("stderr=%q", errOut)
	}
	// Draft-07 and $id.
	code, out, _ = run(t, "ir", "export", p, "--draft", "07", "--id", "https://x/y.json")
	if code != ExitOK || !strings.Contains(out, `"definitions"`) || !strings.Contains(out, `"$id": "https://x/y.json"`) {
		t.Fatalf("code=%d out=%q", code, out)
	}
	// --out writes the file and reports.
	outPath := filepath.Join(t.TempDir(), "user.schema.json")
	code, out, _ = run(t, "ir", "export", p, "--out", outPath)
	if code != ExitOK || !strings.HasPrefix(out, "wrote ") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if data, err := os.ReadFile(outPath); err != nil || !strings.Contains(string(data), `"title": "User"`) {
		t.Fatalf("file: %v %s", err, data)
	}
	// --json envelope carries schema + manifest + portable flag.
	code, out, _ = run(t, "ir", "export", p, "--json")
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Draft    string `json:"draft"`
			Portable bool   `json:"portable"`
			Manifest struct {
				Entries []map[string]any `json:"entries"`
			} `json:"manifest"`
			Schema map[string]any `json:"schema"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK || env.Data.Portable || len(env.Data.Manifest.Entries) != 6 || env.Data.Schema["title"] != "User" || env.Data.Draft != "2020" {
		t.Fatalf("envelope %+v", env.Data)
	}
	// A fully portable document prints no manifest unless asked.
	portable := writeTemp(t, `{"version":"`+"ubgo.validator.ir/v1"+`","root":{"kind":"string","rules":[{"kind":"email"}]}}`)
	code, _, errOut = run(t, "ir", "export", portable)
	if code != ExitOK || errOut != "" {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	_, _, errOut = run(t, "ir", "export", portable, "--manifest")
	if !strings.Contains(errOut, "0 non-portable") {
		t.Fatalf("stderr=%q", errOut)
	}
}

func TestIRExportTS(t *testing.T) {
	p := goldenIR(t)
	tsPath := filepath.Join(t.TempDir(), "user.schema.ts")
	code, out, _ := run(t, "ir", "export", p, "--ts", tsPath, "--out", filepath.Join(t.TempDir(), "s.json"))
	if code != ExitOK || !strings.Contains(out, "userSchema, userManifest") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	data, err := os.ReadFile(tsPath)
	if err != nil || !strings.Contains(string(data), "export const userSchema = {") || !strings.Contains(string(data), "export const userManifest = {") {
		t.Fatalf("ts module: %v %s", err, data)
	}
	if code, out, _ := run(t, "ir", "export", p, "--ts", tsPath, "--name", "acct", "--json"); code != ExitOK || !strings.Contains(out, `"ts":"`+tsPath+`"`) {
		t.Fatalf("named: code=%d out=%q", code, out)
	}
	if data, _ := os.ReadFile(tsPath); !strings.Contains(string(data), "acctSchema") {
		t.Error("custom name not used")
	}
	if code, out, _ := run(t, "ir", "export", p, "--ts", filepath.Join(t.TempDir(), "no", "x.ts"), "--json"); code != ExitFailure || !strings.Contains(out, `"code":"io"`) {
		t.Errorf("unwritable ts: code=%d out=%q", code, out)
	}
	if tsName("") != "schema" || tsName("User") != "user" || tsName("X") != "x" {
		t.Error("tsName")
	}
}

func TestIRExportErrors(t *testing.T) {
	p := goldenIR(t)
	if code, out, _ := run(t, "ir", "export", p, "--draft", "99", "--json"); code != ExitFailure || !strings.Contains(out, `"code":"usage"`) {
		t.Errorf("bad draft: code=%d out=%q", code, out)
	}
	if code, out, _ := run(t, "ir", "export", filepath.Join(t.TempDir(), "nope.json"), "--json"); code != ExitFailure || !strings.Contains(out, `"code":"io"`) {
		t.Errorf("missing file: code=%d out=%q", code, out)
	}
	if code, out, _ := run(t, "ir", "export", writeTemp(t, `{"version":"x","root":{"kind":"nope"}}`), "--json"); code != ExitFailure || !strings.Contains(out, `"code":"decode"`) {
		t.Errorf("decode: code=%d out=%q", code, out)
	}
	if code, out, _ := run(t, "ir", "export", writeTemp(t, `{"version":"v0","root":{"kind":"any"}}`), "--json"); code != ExitFailure || !strings.Contains(out, `"code":"invalid"`) {
		t.Errorf("invalid: code=%d out=%q", code, out)
	}
	// Unwritable --out.
	if code, out, _ := run(t, "ir", "export", p, "--out", filepath.Join(t.TempDir(), "missing-dir", "x.json"), "--json"); code != ExitFailure || !strings.Contains(out, `"code":"io"`) {
		t.Errorf("unwritable out: code=%d out=%q", code, out)
	}
}

func TestFields(t *testing.T) {
	dir := t.TempDir()
	src := "package m\n\nimport \"time\"\n\ntype M struct {\n\tWhen time.Time `json:\"when\"`\n\tsecret string\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// Dry run prints the source and writes nothing.
	code, out, _ := run(t, "fields", dir, "--dry-run")
	if code != ExitOK || !strings.Contains(out, "var MFields = struct") || !strings.Contains(out, "skipped: M.secret") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "fields.gen.go")); err == nil {
		t.Fatal("dry run must not write")
	}
	// Real run writes and reports.
	code, out, _ = run(t, "fields", dir, "--unexported", "--suffix", "Cols", "--json")
	if code != ExitOK {
		t.Fatalf("code=%d out=%q", code, out)
	}
	var env struct {
		OK   bool         `json:"ok"`
		Data fieldsReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK || !env.Data.Written || len(env.Data.Types) != 1 || env.Data.Types[0] != "M" || len(env.Data.Skipped) != 0 {
		t.Fatalf("report %+v", env.Data)
	}
	gen, err := os.ReadFile(filepath.Join(dir, "fields.gen.go"))
	if err != nil || !strings.Contains(string(gen), "var MCols = struct") || !strings.Contains(string(gen), "secret vb.FieldRef[M, string]") {
		t.Fatalf("generated: %v %s", err, gen)
	}
	// Human mode on a second run reports the path; --type filters.
	code, out, _ = run(t, "fields", dir, "--type", "M", "--out", "alt.gen.go")
	if code != ExitOK || !strings.HasPrefix(out, "wrote ") || !strings.Contains(out, "alt.gen.go") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	// Errors: missing type, bad dir.
	if code, out, _ := run(t, "fields", dir, "--type", "Nope", "--json"); code != ExitFailure || !strings.Contains(out, `"code":"invalid"`) {
		t.Fatalf("missing type: code=%d out=%q", code, out)
	}
	if code, _, _ := run(t, "fields", filepath.Join(dir, "missing")); code != ExitFailure {
		t.Fatalf("bad dir: code=%d", code)
	}
	if code, _, _ := run(t, "fields", dir, "extra"); code != ExitUsage {
		t.Fatalf("too many args: code=%d", code)
	}
}

func TestGen(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte("package m\n\ntype M struct {\n\tName string `json:\"name\"`\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	schema := writeTemp(t, `{"version":"ubgo.validator.ir/v1","meta":{"title":"M"},"root":{"kind":"object","fields":[{"name":"Name","json":"name","type":{"kind":"string","rules":[{"kind":"minLength","params":{"value":2}}]}}]}}`)
	code, out, _ := run(t, "gen", dir, "--schema", schema, "--dry-run")
	if code != ExitOK || !strings.Contains(out, "func ValidateM(") || !strings.Contains(out, "rt.RuneLen(v.Name)") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = run(t, "gen", dir, "--schema", schema, "--json")
	if code != ExitOK {
		t.Fatalf("code=%d out=%q", code, out)
	}
	var env struct {
		OK   bool      `json:"ok"`
		Data genReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK || !env.Data.Written || len(env.Data.Funcs) != 1 || env.Data.Funcs[0] != "ValidateM" {
		t.Fatalf("report %+v", env.Data)
	}
	if _, err := os.Stat(filepath.Join(dir, "validate.gen.go")); err != nil {
		t.Fatal("file not written")
	}
	code, out, _ = run(t, "gen", dir, "--schema", schema, "--out", "alt.gen.go")
	if code != ExitOK || !strings.HasPrefix(out, "wrote ") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	// Errors: missing schema flag, unreadable schema, bad IR, unsupported node.
	if code, _, _ := run(t, "gen", dir); code != ExitUsage {
		t.Errorf("missing --schema: code=%d", code)
	}
	if code, out, _ := run(t, "gen", dir, "--schema", filepath.Join(dir, "nope.json"), "--json"); code != ExitFailure || !strings.Contains(out, `"code":"io"`) {
		t.Errorf("missing file: code=%d out=%q", code, out)
	}
	if code, out, _ := run(t, "gen", dir, "--schema", writeTemp(t, `{"version":"x","root":{"kind":"nope"}}`), "--json"); code != ExitFailure || !strings.Contains(out, `"code":"decode"`) {
		t.Errorf("decode: code=%d out=%q", code, out)
	}
	bad := writeTemp(t, `{"version":"ubgo.validator.ir/v1","meta":{"title":"Ghost"},"root":{"kind":"object","fields":[]}}`)
	if code, out, _ := run(t, "gen", dir, "--schema", bad, "--json"); code != ExitFailure || !strings.Contains(out, `"code":"invalid"`) || !strings.Contains(out, "struct Ghost not found") {
		t.Errorf("invalid: code=%d out=%q", code, out)
	}
}

func TestMigrate(t *testing.T) {
	dir := t.TempDir()
	src := "package m\n\ntype M struct {\n\tEmail string `json:\"email\" validate:\"required,email\"`\n\tAge int `validate:\"gte=18\"`\n\tOdd string `validate:\"eqfield=Email\"`\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// Lossy migration fails unless accepted; the report still lists everything.
	code, out, _ := run(t, "migrate", dir, "--type", "M", "--json")
	if code != ExitFailure {
		t.Fatalf("lossy migration must fail: code=%d out=%q", code, out)
	}
	var env struct {
		OK   bool          `json:"ok"`
		Data migrateReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK || env.Data.Lossless || len(env.Data.Unsupported) != 1 || !strings.Contains(env.Data.Unsupported[0], "eqfield") {
		t.Fatalf("report %+v", env.Data)
	}
	// Accepted partial migration writes IR and builder source.
	irPath := filepath.Join(dir, "m.ir.json")
	code, out, _ = run(t, "migrate", dir, "--type", "M", "--out", irPath, "--builder", "schema.gen.go", "--allow-partial")
	if code != ExitOK || !strings.Contains(out, "wrote "+irPath) || !strings.Contains(out, "unsupported: M.Odd") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if data, err := os.ReadFile(irPath); err != nil || !strings.Contains(string(data), `"title": "M"`) {
		t.Fatalf("ir file: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "schema.gen.go")); err != nil || !strings.Contains(string(data), "var MSchema = vb.Struct(") || !strings.Contains(string(data), "package m") {
		t.Fatalf("builder file: %v %s", err, data)
	}
	// Dry run prints both.
	code, out, _ = run(t, "migrate", dir, "--type", "M", "--builder", "x.go", "--dry-run", "--allow-partial")
	if code != ExitOK || !strings.Contains(out, `"version": "ubgo.validator.ir/v1"`) || !strings.Contains(out, "var MSchema") {
		t.Fatalf("dry run: code=%d out=%q", code, out)
	}
	// Errors.
	if code, _, _ := run(t, "migrate", dir); code != ExitUsage {
		t.Errorf("missing --type: code=%d", code)
	}
	if code, out, _ := run(t, "migrate", dir, "--type", "Nope", "--json"); code != ExitFailure || !strings.Contains(out, `"code":"invalid"`) {
		t.Errorf("missing type: code=%d out=%q", code, out)
	}
	if code, _, _ := run(t, "migrate", dir, "--type", "M", "--out", filepath.Join(dir, "no", "x.json"), "--allow-partial"); code != ExitFailure {
		t.Errorf("unwritable out: code=%d", code)
	}
	if code, _, _ := run(t, "migrate", dir, "--type", "M", "--builder", filepath.Join("no", "x.go"), "--allow-partial"); code != ExitFailure {
		t.Errorf("unwritable builder: code=%d", code)
	}
	if _, err := packageName(t.TempDir()); err == nil {
		t.Error("packageName on empty dir")
	}
}

func TestCatalog(t *testing.T) {
	code, out, _ := run(t, "catalog")
	if code != ExitOK || !strings.HasPrefix(out, "{\n  \"locale\": \"en\"") || !strings.Contains(out, `"min_length"`) {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code, out, _ = run(t, "catalog", "--json")
	if code != ExitOK || !strings.Contains(out, `"ok":true`) || !strings.Contains(out, `"locale":"en"`) {
		t.Fatalf("code=%d out=%q", code, out)
	}
	partial := writeTemp(t, `{"locale":"de","messages":{"required":"Pflichtfeld"}}`)
	code, out, _ = run(t, "catalog", "--check", partial)
	if code != ExitFailure || !strings.Contains(out, "de: 1 codes translated") || !strings.Contains(out, "missing: email") {
		t.Fatalf("partial: code=%d out=%q", code, out)
	}
	code, out, _ = run(t, "catalog", "--check", partial, "--json")
	var env struct {
		OK   bool          `json:"ok"`
		Data catalogReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != ExitFailure || !env.OK || len(env.Data.Missing) == 0 {
		t.Fatalf("partial json: code=%d out=%q err=%v", code, out, err)
	}
	// A complete translation passes.
	var full struct {
		Locale   string            `json:"locale"`
		Messages map[string]string `json:"messages"`
	}
	_, def, _ := run(t, "catalog")
	if err := json.Unmarshal([]byte(def), &full); err != nil {
		t.Fatal(err)
	}
	full.Locale = "fr"
	fullDoc, _ := json.Marshal(full)
	if code, out, _ := run(t, "catalog", "--check", writeTemp(t, string(fullDoc))); code != ExitOK || !strings.Contains(out, "0 missing") {
		t.Fatalf("full: code=%d out=%q", code, out)
	}
	if code, out, _ := run(t, "catalog", "--check", filepath.Join(t.TempDir(), "nope.json"), "--json"); code != ExitFailure || !strings.Contains(out, `"code":"io"`) {
		t.Errorf("missing file: code=%d out=%q", code, out)
	}
	if code, out, _ := run(t, "catalog", "--check", writeTemp(t, `{"locale":"x"}`), "--json"); code != ExitFailure || !strings.Contains(out, `"code":"decode"`) {
		t.Errorf("bad catalog: code=%d out=%q", code, out)
	}
}

func TestIRContract(t *testing.T) {
	code, out, _ := run(t, "ir", "contract")
	if code != ExitOK || !strings.Contains(out, `"$id": "https://ubgo.dev/schemas/validator-ir-v1.json"`) || !strings.Contains(out, `"TypeNode"`) {
		t.Fatalf("code=%d out=%q", code, out[:200])
	}
	code, out, _ = run(t, "ir", "contract", "--json")
	if code != ExitOK || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("json: code=%d", code)
	}
}

func TestIRErrors(t *testing.T) {
	code, out, _ := run(t, "ir", "errors")
	if code != ExitOK {
		t.Fatalf("code = %d", code)
	}
	for _, want := range []string{
		`"$id": "https://ubgo.dev/schemas/validator-errors-v1.json"`,
		`"issues"`,
		`"Issue"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	code, out, _ = run(t, "ir", "errors", "--json")
	if code != ExitOK || !strings.Contains(out, `"ok":true`) {
		t.Fatalf("json: code=%d", code)
	}
}
