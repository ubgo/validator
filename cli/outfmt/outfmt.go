// Package outfmt is the single output seam for every validatorctl verb.
//
// Why it exists: with one writer per mode (human vs --json), verbs cannot
// drift into different envelope shapes. Every byte a verb emits goes through
// a Printer, and the JSON envelope is defined exactly once, here.
//
// The envelope is stable API for scripts:
//
//	{"ok":true,"data":<verb-specific>}
//	{"ok":false,"error":{"code":"<ErrCode>","message":"..."}}
//
// Changing a field name here is a breaking change for every consumer piping
// validatorctl --json into jq — treat it with library-API discipline.
package outfmt

import (
	"encoding/json"
	"fmt"
	"io"
)

// ErrCode classifies a failure in the JSON envelope. Closed set — scripts
// branch on these strings, so adding a value is additive API and renaming
// one is a breaking change.
type ErrCode string

const (
	// CodeIO: a file could not be read or written.
	CodeIO ErrCode = "io"

	// CodeDecode: the input was not a well-formed IR document.
	CodeDecode ErrCode = "decode"

	// CodeInvalid: the IR decoded but failed structural Check.
	CodeInvalid ErrCode = "invalid"

	// CodeUsage: arguments did not parse (missing operand, bad flag).
	CodeUsage ErrCode = "usage"
)

// Printer renders verb output in exactly one of two modes, chosen once at
// root-command parse time.
//
// Invariant: verbs never write to their cobra command's writers directly —
// everything flows through Out so tests can capture it and so the JSON mode
// cannot be polluted by stray human-form prints.
type Printer struct {
	// Out receives ALL success output (human or JSON envelope alike).
	Out io.Writer

	// JSON selects the machine envelope. When false, verbs print their
	// human-readable form via Human/Humanf and OK emits nothing.
	JSON bool
}

// envelope is the wire shape of every --json response. Kept unexported: the
// only way to produce one is OK/Fail, which is what keeps the shape uniform.
type envelope struct {
	OK    bool      `json:"ok"`
	Data  any       `json:"data,omitempty"`
	Error *envError `json:"error,omitempty"`
}

type envError struct {
	Code    ErrCode `json:"code"`
	Message string  `json:"message"`
}

// OK emits a success envelope in JSON mode and nothing in human mode (human
// output was already printed by the verb via Human/Humanf).
func (p Printer) OK(data any) error {
	if !p.JSON {
		return nil
	}
	return p.emit(envelope{OK: true, Data: data})
}

// Fail emits a failure envelope in JSON mode, or the message in human mode.
func (p Printer) Fail(code ErrCode, err error) error {
	if !p.JSON {
		_, werr := fmt.Fprintf(p.Out, "error: %v\n", err)
		return werr
	}
	return p.emit(envelope{OK: false, Error: &envError{Code: code, Message: err.Error()}})
}

// Human prints a line in human mode only.
func (p Printer) Human(s string) {
	if p.JSON {
		return
	}
	_, _ = fmt.Fprintln(p.Out, s)
}

// Humanf prints a formatted line in human mode only.
func (p Printer) Humanf(format string, args ...any) {
	if p.JSON {
		return
	}
	_, _ = fmt.Fprintf(p.Out, format+"\n", args...)
}

func (p Printer) emit(e envelope) error {
	enc := json.NewEncoder(p.Out)
	return enc.Encode(e)
}
