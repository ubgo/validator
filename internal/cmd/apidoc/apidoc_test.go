package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAPIDocIsCurrent is what makes a generated reference trustworthy: it
// fails when docs/API.md no longer matches the source it claims to
// describe. Without it the page is just prose that happens to have been
// accurate once, which is the failure mode this whole file exists to
// avoid.
func TestAPIDocIsCurrent(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	cmd := exec.Command("go", "run", "./internal/cmd/apidoc")
	cmd.Dir = root
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("apidoc failed: %v", err)
	}
	want, err := os.ReadFile(filepath.Join(root, "docs", "API.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("docs/API.md is stale; run `task doc:api`")
	}
}

// TestAPIDocCoversTheSurface guards against the generator silently
// emitting an empty or half-built page: a reference that lists nothing
// would still match its golden.
func TestAPIDocCoversTheSurface(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "API.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(page)

	// Every package declared above must have produced a section.
	for _, p := range packages {
		if !strings.Contains(s, "\n## "+p.dir+"\n") {
			t.Errorf("no section for package %s", p.dir)
		}
	}

	// And a sample of symbols a reader would look for, spanning the
	// shapes that are easy to render wrong: generics, receivers,
	// function-typed parameters and interfaces.
	for _, want := range []string{
		"func Struct[T any](name string, fields ...Field[T]) *StructBuilder[T]",
		"func Value[V any](name string, b Builder[V]) (*Schema[V], error)",
		"func (*StringType) Custom(name string, fn func(context.Context, string) error) *StringType",
		"func FromSchema[T any](s *vb.Schema[T], opts ...Option) (*Validator, error)",
		"func Typed[T any](s *vb.Schema[T], opts ...Option) (*TypedValidator[T], error)",
		"type ExprCompiler interface",
		"func Emit(s *ir.Schema, opts ...Option) (*Result, error)",
		"func As(err error) (*Errors, bool)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the reference is missing %q", want)
		}
	}

	// A page this size should be well over a thousand lines; a sudden
	// collapse means the generator stopped finding declarations.
	if n := strings.Count(s, "\n"); n < 800 {
		t.Errorf("the reference is only %d lines; the generator likely failed silently", n)
	}
}
