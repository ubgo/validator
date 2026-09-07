package accessors

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const exampleDir = "internal/example"

// TestGoldenIsCurrent pins the checked-in fields.gen.go: the example
// package compiles it (the real proof); this test makes sure it is what
// the current generator emits. Regenerate with UPDATE_GOLDEN=1.
func TestGoldenIsCurrent(t *testing.T) {
	res, err := Generate(Options{Dir: exampleDir})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(exampleDir, DefaultOutput)
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, res.Source, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (run with UPDATE_GOLDEN=1 to create)", err)
	}
	if !bytes.Equal(want, res.Source) {
		t.Fatalf("fields.gen.go is stale; run UPDATE_GOLDEN=1 go test ./accessors\n%s", res.Source)
	}
	if strings.Join(res.Types, ",") != "Address,User,Plain" {
		t.Errorf("types %v", res.Types)
	}
	// gofmt aligns struct fields in columns; compare on collapsed spaces.
	src := collapse(string(res.Source))
	for _, want := range []string{
		Header,
		"package example",
		`str "strings"`,
		`"time"`,
		`"github.com/ubgo/validator/vb"`,
		"var UserFields = struct {",
		"Email vb.FieldRef[User, string]",
		"Start vb.FieldRef[User, time.Time]",
		"Nick vb.FieldRef[User, *string]",
		"Labels vb.FieldRef[User, map[string]int]",
		"Home vb.FieldRef[User, *Address]",
		"Builder vb.FieldRef[User, *str.Builder]",
		`vb.NewField("Email", func(v User) string { return v.Email }).JSON("email")`,
		`vb.NewField("Untagged", func(v User) bool { return v.Untagged }),`,
		`vb.NewField("Same", func(v User) string { return v.Same }),`,
		`vb.NewField("Secret", func(v User) string { return v.Secret }),`,
		"// skipped: Box: generic struct",
		"// skipped: User: embedded field Address",
		"// skipped: User.password: unexported field",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in generated source", want)
		}
	}
	if strings.Contains(src, "password vb.FieldRef") || strings.Contains(src, "BoxFields") || strings.Contains(src, "notAStruct") {
		t.Error("skipped items leaked into accessors")
	}
	if len(res.Skipped) != 3 {
		t.Errorf("skipped %v", res.Skipped)
	}
}

func TestOptions(t *testing.T) {
	res, err := Generate(Options{Dir: exampleDir, Types: []string{"Plain"}, Suffix: "Cols", IncludeUnexported: true})
	if err != nil {
		t.Fatal(err)
	}
	src := string(res.Source)
	if !strings.Contains(src, "var PlainCols = struct") || strings.Contains(src, "UserCols") {
		t.Errorf("type filter/suffix: %s", src)
	}
	if strings.Contains(src, `"time"`) {
		t.Error("unused imports must not be emitted")
	}
	// IncludeUnexported emits the password field.
	res, err = Generate(Options{Dir: exampleDir, Types: []string{"User"}, IncludeUnexported: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(collapse(string(res.Source)), "password vb.FieldRef[User, string]") {
		t.Error("unexported field not included")
	}
	// A type that does not exist is an error, not silence.
	if _, err := Generate(Options{Dir: exampleDir, Types: []string{"Nope", "User"}}); err == nil || !strings.Contains(err.Error(), "not found in package: Nope") {
		t.Errorf("missing type: %v", err)
	}
	if _, err := Generate(Options{}); err == nil {
		t.Error("Dir required")
	}
	if _, err := Generate(Options{Dir: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Error("missing dir")
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestEdgeCases(t *testing.T) {
	// Empty dir, no structs, mixed packages, parse error.
	if _, err := Generate(Options{Dir: writeFiles(t, map[string]string{"README.md": "x"})}); err == nil || !strings.Contains(err.Error(), "no Go package") {
		t.Errorf("empty: %v", err)
	}
	if _, err := Generate(Options{Dir: writeFiles(t, map[string]string{"a.go": "package a\ntype T int\n"})}); err == nil || !strings.Contains(err.Error(), "no structs") {
		t.Errorf("no structs: %v", err)
	}
	if _, err := Generate(Options{Dir: writeFiles(t, map[string]string{"a.go": "package a\ntype A struct{X int}\n", "b.go": "package b\n"})}); err == nil || !strings.Contains(err.Error(), "several packages") {
		t.Errorf("mixed: %v", err)
	}
	if _, err := Generate(Options{Dir: writeFiles(t, map[string]string{"a.go": "package a\ntype A struct{"})}); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("parse error: %v", err)
	}
	// Tests, .gen.go files and the output itself are ignored as input;
	// dot and blank imports are ignored; a bad tag is tolerated; the
	// blank identifier field is skipped.
	dir := writeFiles(t, map[string]string{
		"a.go":          "package a\n\nimport (\n\t. \"fmt\"\n\t_ \"os\"\n)\n\ntype A struct {\n\tX int `json:\"x\"`\n\t_ int\n\tY string `json`\n\tZ Stringer\n}\n",
		"a_test.go":     "package a\ntype FromTest struct{Q int}\n",
		"old.gen.go":    "package a\ntype FromGen struct{Q int}\n",
		"fields.gen.go": "package a\ntype FromOut struct{Q int}\n",
	})
	res, err := Generate(Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	src := collapse(string(res.Source))
	if strings.Contains(src, "FromTest") || strings.Contains(src, "FromGen") || strings.Contains(src, "FromOut") {
		t.Error("ignored inputs leaked")
	}
	if !strings.Contains(src, "Z vb.FieldRef[A, Stringer]") || !strings.Contains(src, `"X", func(v A) int { return v.X }).JSON("x")`) || strings.Contains(src, "_ vb.FieldRef") {
		t.Errorf("edge source:\n%s", src)
	}
	if strings.Contains(src, `"fmt"`) || strings.Contains(src, `"os"`) {
		t.Error("dot/blank imports must not be carried")
	}
}

func TestWriteIsIdempotent(t *testing.T) {
	dir := writeFiles(t, map[string]string{"m.go": "package m\n\nimport \"time\"\n\ntype M struct {\n\tWhen time.Time `json:\"when\"`\n}\n"})
	res, err := Write(Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	first, err := os.Stat(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	second, _ := os.Stat(res.Path)
	if !second.ModTime().Equal(first.ModTime()) {
		t.Error("unchanged content must not be rewritten")
	}
	// Re-generation reads the source files only: the output is excluded
	// from its own input, so a second run is stable.
	again, _ := Generate(Options{Dir: dir})
	if !bytes.Equal(again.Source, res.Source) {
		t.Error("generation is not stable across runs")
	}
	// Unwritable target.
	if _, err := Write(Options{Dir: dir, Output: filepath.Join("no-such-dir", "x.go")}); err == nil {
		t.Error("write into a missing directory must fail")
	}
}

// collapse folds runs of spaces so column-aligned output compares stably.
func collapse(s string) string {
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}

func TestJSONName(t *testing.T) {
	if jsonName(nil, "A") != "" {
		t.Error("nil tag")
	}
}

// TestImportsResolveByPackageName covers the case that made generated
// accessors fail to compile: a package whose NAME differs from its
// DIRECTORY. A file importing `x/vendorlib` that declares `package money`
// writes `money.Amount`, and a generator guessing the identifier from the
// path emitted a file using `money` while importing nothing that defines
// it.
//
// Two names for one path is the second half: the same package reached
// through its own name and an alias needs both import lines, or one of
// the two field types has no package in scope.
func TestImportsResolveByPackageName(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module accprobe\n\ngo 1.24\n")
	write("vendorlib/lib.go", "// Package money's name differs from its directory.\npackage money\n\ntype Amount struct{ Cents int64 }\n")
	write("models/user.go", `package models

import (
	"accprobe/vendorlib"
	alias "accprobe/vendorlib"
)

type User struct {
	Paid  money.Amount
	Owed  alias.Amount
	Notes string
}
`)

	res, err := Generate(Options{Dir: filepath.Join(dir, "models")})
	if err != nil {
		t.Fatal(err)
	}
	src := string(res.Source)

	// The field types name both identifiers...
	for _, want := range []string{"money.Amount", "alias.Amount"} {
		if !strings.Contains(src, want) {
			t.Errorf("generated source lacks %q:\n%s", want, src)
		}
	}
	// ...so both must be imported, one of them aliased.
	if !strings.Contains(src, `"accprobe/vendorlib"`) {
		t.Error("the package's own name is not imported")
	}
	if !strings.Contains(src, `alias "accprobe/vendorlib"`) {
		t.Error("the alias is not imported")
	}
	// And the plain import must NOT carry a redundant alias: the package
	// is called money, so `money "accprobe/vendorlib"` would be noise.
	if strings.Contains(src, `money "accprobe/vendorlib"`) {
		t.Error("a redundant alias was written for the package's own name")
	}

	// The real proof: it parses as Go. A generator that emits a file
	// naming an unimported package produces something that never builds,
	// which is exactly the bug this test exists for.
	if _, err := parser.ParseFile(token.NewFileSet(), "fields.gen.go", src, parser.AllErrors); err != nil {
		t.Fatalf("generated source does not parse: %v\n%s", err, src)
	}
}

// TestPackageNameResolutionIsBestEffort pins that resolution degrades
// rather than fails: a path outside the module keeps the guess, which is
// right for the standard library and every ordinary dependency.
func TestPackageNameResolutionIsBestEffort(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module m\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	p := &Package{fset: token.NewFileSet(), dir: sub}

	if _, ok := p.packageName("time"); ok {
		t.Error("a stdlib path should not resolve to a directory")
	}
	if _, ok := p.packageName("github.com/other/mod/pkg"); ok {
		t.Error("a path outside the module should not resolve")
	}
	if _, ok := p.packageName("m/missing"); ok {
		t.Error("a path inside the module with no directory should not resolve")
	}
	// And the cache returns the same answer twice.
	if _, ok := p.packageName("time"); ok {
		t.Error("cached negative result changed")
	}
}
