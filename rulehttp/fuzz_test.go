package rulehttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ubgo/validator/vb"
)

// The handler is the one part of this library that reads bytes off the
// public internet, so its contract has to hold for every body, not just
// well-formed ones.

type fuzzAccount struct {
	Handle string `json:"handle"`
}

var fuzzHandle = vb.NewField("Handle", func(a fuzzAccount) string { return a.Handle }).JSON("handle")

var fuzzSchema = vb.Struct("Account",
	vb.F(fuzzHandle, vb.String().MinLength(3).
		Async("handle_free", "/v/handle_free", func(_ context.Context, h string) error {
			if h == "taken" {
				return errors.New("already taken")
			}
			return nil
		})),
).MustBuild()

// okStatuses is every status the handler is allowed to answer. A status
// outside this set means a path returned something undesigned.
var okStatuses = map[int]bool{
	http.StatusOK:                    true,
	http.StatusBadRequest:            true,
	http.StatusForbidden:             true,
	http.StatusNotFound:              true,
	http.StatusMethodNotAllowed:      true,
	http.StatusRequestEntityTooLarge: true,
	http.StatusTooManyRequests:       true,
	http.StatusInternalServerError:   true,
}

// FuzzServeHTTP pins the wire contract the TypeScript runtime depends on:
//
//   - Every response is valid JSON with the documented shape. The frontend
//     parses it unconditionally, so a body it cannot parse would surface as
//     a hard failure in the browser rather than a validation message.
//   - ok=true carries no issues and no error; ok=false carries at least one
//     of them. A response that is neither leaves the caller unable to tell
//     "valid" from "could not check", which is the one distinction that
//     must never blur — the backend is authoritative and a lost error must
//     not read as a pass.
//   - The status is one the handler designed for.
func FuzzServeHTTP(f *testing.F) {
	for _, seed := range []string{
		`{"rule":"handle_free","value":"free"}`,
		`{"rule":"handle_free","value":"taken"}`,
		`{"value":"bare"}`,
		`{"rule":"other","value":"x"}`,
		`{"value":{"nested":[1,2]}}`,
		`{"value":null}`,
		`{`,
		``,
		`[]`,
		`{"unknown":1}`,
	} {
		f.Add([]byte(seed))
	}

	h, err := FromSchema(fuzzSchema, WithLimiter(NoLimit))
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		req := httptest.NewRequest(http.MethodPost, "/v/handle_free", strings.NewReader(string(body)))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if !okStatuses[rec.Code] {
			t.Fatalf("undesigned status %d for body %q", rec.Code, body)
		}
		var resp Response
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("response is not valid JSON: %v\nbody %q\nresponse %q", err, body, rec.Body.String())
		}
		switch {
		case resp.OK && (len(resp.Issues) > 0 || resp.Error != nil):
			t.Fatalf("ok=true with issues or an error: %+v (body %q)", resp, body)
		case !resp.OK && len(resp.Issues) == 0 && resp.Error == nil:
			t.Fatalf("ok=false with neither issues nor an error: %+v (body %q)", resp, body)
		}
		if resp.Error != nil && resp.Error.Code == "" {
			t.Fatalf("error with no code: %+v (body %q)", resp, body)
		}
		for _, is := range resp.Issues {
			if is.Code == "" {
				t.Fatalf("issue with no code: %+v (body %q)", is, body)
			}
		}
		// A 200 is only ever a decided answer, never a transport failure.
		if rec.Code == http.StatusOK && resp.Error != nil {
			t.Fatalf("200 carrying a transport error: %+v (body %q)", resp, body)
		}
	})
}

// FuzzServeHTTPPaths fuzzes the request line rather than the body: an
// unknown path or a wrong method must be a designed refusal, never a hit
// on another rule. This is what keeps the handler from being an
// enumeration oracle for rules it does not serve.
func FuzzServeHTTPPaths(f *testing.F) {
	f.Add("/v/handle_free", "POST")
	f.Add("/v/not_reserved", "POST")
	f.Add("/v/handle_free", "GET")
	f.Add("/", "POST")
	f.Add("/v/handle_free/../other", "POST")

	h, err := FromSchema(fuzzSchema, WithLimiter(NoLimit))
	if err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, path, method string) {
		// http.NewRequest validates the request line and returns an error;
		// httptest.NewRequest panics instead, which would report a harness
		// bug as a library failure.
		req, err := http.NewRequest(method, "http://example.com"+path, strings.NewReader(`{"value":"free"}`))
		if err != nil {
			return
		}
		req.RemoteAddr = "203.0.113.7:1234" // the default client key reads this
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if !okStatuses[rec.Code] {
			t.Fatalf("undesigned status %d for %s %s", rec.Code, method, path)
		}
		// Only the declared endpoint, reached with POST, may answer 200.
		if rec.Code == http.StatusOK && (req.URL.Path != "/v/handle_free" || method != http.MethodPost) {
			t.Fatalf("200 for %s %s (resolved path %q), which is not the declared endpoint", method, path, req.URL.Path)
		}
	})
}
