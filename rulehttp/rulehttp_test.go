package rulehttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/validator/vb"
	"github.com/ubgo/validator/verr"
)

type User struct {
	Email string
	Name  string
}

var (
	fEmail = vb.NewField("Email", func(u User) string { return u.Email }).JSON("email")
	fName  = vb.NewField("Name", func(u User) string { return u.Name }).JSON("name")
)

func schema() *vb.Schema[User] {
	return vb.Struct("User",
		vb.F(fEmail, vb.String().Email().
			Async("unique_email", "/v/unique_email", func(_ context.Context, s string) error {
				if s == "taken@example.com" {
					return errors.New("already taken")
				}
				return nil
			}).
			Custom("no_temp", func(context.Context, string) error { return nil })),
		vb.F(fName, vb.String().Async("polite", "", func(_ context.Context, s string) error {
			if strings.Contains(s, "rude") {
				return &verr.Errors{Issues: []verr.Issue{{Code: "rude", Path: "$.name", Message: "be polite"}}}
			}
			return nil
		})),
	).MustBuild()
}

func newHandler(t *testing.T, opts ...Option) *Handler {
	t.Helper()
	h, err := FromSchema(schema(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func post(h http.Handler, path, body string) (int, Response) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.RemoteAddr = "203.0.113.7:4242"
	h.ServeHTTP(rec, req)
	var res Response
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return rec.Code, res
}

func TestPathsAndMount(t *testing.T) {
	h := newHandler(t)
	if got := h.Paths(); strings.Join(got, ",") != "/v/polite,/v/unique_email" {
		t.Fatalf("paths %v", got)
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	code, res := post(mux, "/v/unique_email", `{"rule":"unique_email","value":"free@example.com"}`)
	if code != http.StatusOK || !res.OK {
		t.Fatalf("mounted: %d %+v", code, res)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v/unique_email", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("mux GET: %d", rec.Code)
	}
}

func TestRoundTrip(t *testing.T) {
	h := newHandler(t)
	code, res := post(h, "/v/unique_email", `{"rule":"unique_email","value":"taken@example.com"}`)
	if code != http.StatusOK || res.OK || len(res.Issues) != 1 || res.Issues[0].Code != verr.CodeCustom || res.Issues[0].Message != "already taken" || res.Issues[0].Rule != "async" || res.Issues[0].Params["ref"] != "unique_email" {
		t.Fatalf("%d %+v", code, res)
	}
	// A *verr.Errors from the body passes through verbatim.
	code, res = post(h, "/v/polite", `{"value":"you are rude"}`)
	if code != http.StatusOK || res.OK || res.Issues[0].Code != "rude" || res.Issues[0].Path != "$.name" {
		t.Fatalf("%d %+v", code, res)
	}
	// Rule omitted in the body is fine; a mismatching rule is not.
	if code, res = post(h, "/v/polite", `{"value":"hi"}`); code != http.StatusOK || !res.OK {
		t.Fatalf("%d %+v", code, res)
	}
	if code, res = post(h, "/v/polite", `{"rule":"unique_email","value":"hi"}`); code != http.StatusBadRequest || res.Error.Code != CodeBadRequest {
		t.Fatalf("%d %+v", code, res)
	}
}

func TestTransportErrors(t *testing.T) {
	h := newHandler(t)
	cases := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		code   string
	}{
		{"unknown path", http.MethodPost, "/v/nope", `{}`, http.StatusNotFound, CodeUnknownRule},
		{"custom rules are never served", http.MethodPost, "/v/no_temp", `{}`, http.StatusNotFound, CodeUnknownRule},
		{"wrong method", http.MethodGet, "/v/polite", ``, http.StatusMethodNotAllowed, CodeMethod},
		{"bad json", http.MethodPost, "/v/polite", `{"value":`, http.StatusBadRequest, CodeBadRequest},
		{"unknown field", http.MethodPost, "/v/polite", `{"value":"x","extra":1}`, http.StatusBadRequest, CodeBadRequest},
		{"wrong value type", http.MethodPost, "/v/polite", `{"value":42}`, http.StatusBadRequest, CodeBadValue},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
			h.ServeHTTP(rec, req)
			var res Response
			_ = json.Unmarshal(rec.Body.Bytes(), &res)
			if rec.Code != c.status || res.Error == nil || res.Error.Code != c.code {
				t.Fatalf("got %d %+v", rec.Code, res)
			}
			if c.method == http.MethodGet && rec.Header().Get("Allow") != http.MethodPost {
				t.Error("Allow header missing")
			}
		})
	}
}

func TestBodyLimit(t *testing.T) {
	h := newHandler(t, WithMaxBody(32))
	big := `{"value":"` + strings.Repeat("a", 100) + `"}`
	if code, res := post(h, "/v/polite", big); code != http.StatusRequestEntityTooLarge || res.Error.Code != CodeTooLarge {
		t.Fatalf("%d %+v", code, res)
	}
	if code, _ := post(h, "/v/polite", `{"value":"ok"}`); code != http.StatusOK {
		t.Fatalf("small body rejected: %d", code)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestUnreadableBody(t *testing.T) {
	h := newHandler(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v/polite", errReader{})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("%d", rec.Code)
	}
}

func TestAuthorizeAndClientKey(t *testing.T) {
	var seenKey string
	h := newHandler(t,
		WithAuthorize(func(r *http.Request) error {
			if r.Header.Get("Authorization") == "" {
				return errors.New("login required")
			}
			return nil
		}),
		WithLimiter(NewTokenBucket(1, 1)),
		WithClientKey(func(r *http.Request) string { seenKey = r.Header.Get("X-Client"); return seenKey }),
	)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v/polite", strings.NewReader(`{"value":"x"}`)))
	var res Response
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != http.StatusForbidden || res.Error.Code != CodeUnauthorized || res.Error.Message != "login required" {
		t.Fatalf("%d %+v", rec.Code, res)
	}
	send := func(client string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v/polite", strings.NewReader(`{"value":"x"}`))
		req.Header.Set("Authorization", "Bearer t")
		req.Header.Set("X-Client", client)
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if send("a") != http.StatusOK || seenKey != "a" {
		t.Error("first request must pass")
	}
	if send("a") != http.StatusTooManyRequests {
		t.Error("second request within the window must be limited")
	}
	if send("b") != http.StatusOK {
		t.Error("another client has its own bucket")
	}
}

func TestDefaultLimiterAndRemoteIP(t *testing.T) {
	h := newHandler(t)
	// Default burst is 20; the 21st rapid request from one IP is limited.
	var last int
	for range DefaultBurst + 1 {
		last, _ = post(h, "/v/polite", `{"value":"x"}`)
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("default limiter did not engage: %d", last)
	}
	if remoteIP(&http.Request{RemoteAddr: "10.0.0.1:99"}) != "10.0.0.1" || remoteIP(&http.Request{RemoteAddr: "weird"}) != "weird" {
		t.Error("remoteIP")
	}
	if !NoLimit.Allow("anyone") {
		t.Error("NoLimit")
	}
}

func TestNewValidation(t *testing.T) {
	nop := func(context.Context, any) error { return nil }
	if _, err := New([]vb.Endpoint{{Rule: "a", Path: "/v/a"}}); err == nil {
		t.Error("nil func accepted")
	}
	if _, err := New([]vb.Endpoint{{Rule: "a", Path: "/v/x", Func: nop}, {Rule: "b", Path: "/v/x", Func: nop}}); err == nil {
		t.Error("path collision accepted")
	}
	h, err := New([]vb.Endpoint{{Rule: "a", Path: "/v/x", Func: nop}, {Rule: "a", Path: "/v/x", Func: nop}})
	if err != nil || len(h.Paths()) != 1 {
		t.Errorf("same rule twice must dedupe: %v", err)
	}
	if got := issuesOf(&verr.Errors{}, "r"); len(got) != 1 || got[0].Code != verr.CodeCustom {
		t.Error("empty Errors must fall back to one custom issue")
	}
}

func TestTokenBucket(t *testing.T) {
	now := time.Unix(0, 0)
	tb := NewTokenBucket(2, 3) // 2/s, burst 3
	tb.now = func() time.Time { return now }
	for i := range 3 {
		if !tb.Allow("k") {
			t.Fatalf("burst request %d refused", i)
		}
	}
	if tb.Allow("k") {
		t.Fatal("bucket should be empty")
	}
	now = now.Add(500 * time.Millisecond) // +1 token
	if !tb.Allow("k") || tb.Allow("k") {
		t.Fatal("refill by rate")
	}
	now = now.Add(time.Hour) // refill caps at burst
	for range 3 {
		if !tb.Allow("k") {
			t.Fatal("refill must cap at burst, not below")
		}
	}
	if tb.Allow("k") {
		t.Fatal("refill must cap at burst, not above")
	}
}

func TestTokenBucketEviction(t *testing.T) {
	now := time.Unix(0, 0)
	tb := NewTokenBucket(1, 1)
	tb.now = func() time.Time { return now }
	for i := range maxBucketKeys {
		tb.Allow("client-" + string(rune('a'+i%26)) + "-" + itoa(i))
	}
	if len(tb.buckets) != maxBucketKeys {
		t.Fatalf("expected %d buckets, got %d", maxBucketKeys, len(tb.buckets))
	}
	// All idle long enough to be full: they are dropped before adding a new one.
	now = now.Add(time.Minute)
	tb.Allow("newcomer")
	if len(tb.buckets) != 1 {
		t.Fatalf("idle buckets not evicted: %d", len(tb.buckets))
	}
	// Refill the map with ACTIVE clients (touched just now), then overflow:
	// half are dropped.
	for i := range maxBucketKeys {
		tb.Allow("active-" + itoa(i))
	}
	tb.Allow("one-more")
	if n := len(tb.buckets); n > maxBucketKeys/2+2 {
		t.Fatalf("active eviction did not halve the map: %d", n)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
