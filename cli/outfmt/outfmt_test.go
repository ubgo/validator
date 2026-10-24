package outfmt

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestHumanMode(t *testing.T) {
	var buf bytes.Buffer
	p := Printer{Out: &buf}
	p.Human("hello")
	p.Humanf("n=%d", 3)
	if err := p.OK(map[string]int{"x": 1}); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "hello\nn=3\n" {
		t.Fatalf("human output %q — OK must print nothing in human mode", got)
	}
	buf.Reset()
	if err := p.Fail(CodeIO, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "error: boom\n" {
		t.Fatalf("fail output %q", got)
	}
}

func TestJSONMode(t *testing.T) {
	var buf bytes.Buffer
	p := Printer{Out: &buf, JSON: true}
	p.Human("must not appear")
	p.Humanf("nor %s", "this")
	if err := p.OK(map[string]int{"x": 1}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != `{"ok":true,"data":{"x":1}}` {
		t.Fatalf("ok envelope %q", got)
	}
	buf.Reset()
	if err := p.Fail(CodeDecode, errors.New("bad")); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != `{"ok":false,"error":{"code":"decode","message":"bad"}}` {
		t.Fatalf("fail envelope %q", got)
	}
}

// failingWriter surfaces write errors so callers can see the seam reports
// them instead of swallowing.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestWriteErrorsSurface(t *testing.T) {
	p := Printer{Out: failingWriter{}, JSON: true}
	if err := p.OK(nil); err == nil {
		t.Error("OK must return the write error")
	}
	p.JSON = false
	if err := p.Fail(CodeUsage, errors.New("x")); err == nil {
		t.Error("Fail must return the write error")
	}
}
