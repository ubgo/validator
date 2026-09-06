// Package rulehttp serves registered async rules over HTTP — the server
// side of the round-trip contract the TypeScript runtime speaks
// (docsi/MISSION.md ADR-VAL-005, tier 3).
//
// One rule, two consumers: the Go executor runs an async body directly
// against the database; the frontend reaches the SAME body through
// `POST <endpoint>` with `{"rule": name, "value": v}` and gets back
// `{"ok": true}` or `{"ok": false, "issues": [...]}`. No logic is
// duplicated, and the backend stays authoritative — a full Validate() on
// submit re-checks everything.
//
// Security posture, because a uniqueness check is an enumeration oracle
// ("is this email registered?"): only async rules are served, never custom
// ones; nothing is mounted unless the caller asks; request bodies are
// capped; a per-client rate limiter is on by default; and an authorization
// hook runs before any rule. The careless path is the safe path.
package rulehttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"

	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
	"github.com/ubgo/validator/verr"
)

// Wire shapes, mirrored by ts/src/index.ts (ServerRequest/ServerResponse).

// Request is the round-trip body.
type Request struct {
	// Rule is the registered function name. Must match the endpoint's rule
	// when present; omitted is accepted so a bare `{"value": …}` works.
	Rule string `json:"rule,omitempty"`

	// Value is the field value under validation, JSON-decoded.
	Value any `json:"value"`
}

// Response is the round-trip result.
type Response struct {
	OK     bool         `json:"ok"`
	Issues []verr.Issue `json:"issues,omitempty"`
	Error  *ErrorBody   `json:"error,omitempty"`
}

// ErrorBody describes a transport-level failure (not a validation result).
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Transport error codes. Closed set; the TS runtime and scripts branch on
// them.
const (
	CodeUnknownRule  = "unknown_rule"
	CodeMethod       = "method_not_allowed"
	CodeBadRequest   = "bad_request"
	CodeBadValue     = "bad_value"
	CodeTooLarge     = "too_large"
	CodeRateLimited  = "rate_limited"
	CodeUnauthorized = "unauthorized"
	CodeInternal     = "internal"
)

// DefaultMaxBody caps a request body. Rule values are single fields — an
// email, a username — so 64 KiB is generous and stops a client from making
// the server decode megabytes into memory.
const DefaultMaxBody int64 = 64 << 10

// Default limiter shape: 10 requests per second with a burst of 20 per
// client. A typing user with a 300 ms debounce needs ~3/s; a scraper
// enumerating emails is throttled hard. Override with [WithLimiter].
const (
	DefaultRatePerSecond = 10
	DefaultBurst         = 20
)

// Limiter decides whether a client may proceed. Keys are client
// identities (see [WithClientKey]).
type Limiter interface {
	Allow(key string) bool
}

// Option configures a [Handler].
type Option func(*Handler)

// WithMaxBody sets the request body cap in bytes.
func WithMaxBody(n int64) Option { return func(h *Handler) { h.maxBody = n } }

// WithLimiter replaces the default token bucket. Pass [NoLimit] to disable
// rate limiting deliberately.
func WithLimiter(l Limiter) Option { return func(h *Handler) { h.limiter = l } }

// WithAuthorize installs a hook that runs before every rule. Return an
// error to refuse the request with 403; the error's text is returned to the
// client, so keep it free of internals.
func WithAuthorize(fn func(r *http.Request) error) Option {
	return func(h *Handler) { h.authorize = fn }
}

// WithClientKey replaces how a client is identified for rate limiting.
// Default: the remote IP, honoring none of the proxy headers — set this
// when behind a trusted proxy.
func WithClientKey(fn func(r *http.Request) string) Option {
	return func(h *Handler) { h.clientKey = fn }
}

// Handler serves a set of async rules. Build with [New] and mount it on a
// mux, or use it as the mux: it routes on the request path.
type Handler struct {
	rules     map[string]vb.Endpoint // by path
	maxBody   int64
	limiter   Limiter
	authorize func(*http.Request) error
	clientKey func(*http.Request) string
}

// New builds a handler for the given endpoints. Typically fed from
// [vb.Schema.Endpoints]; several schemas may be combined. Two endpoints on
// the same path with different rules is a configuration error.
func New(endpoints []vb.Endpoint, opts ...Option) (*Handler, error) {
	h := &Handler{
		rules:     make(map[string]vb.Endpoint, len(endpoints)),
		maxBody:   DefaultMaxBody,
		limiter:   NewTokenBucket(DefaultRatePerSecond, DefaultBurst),
		clientKey: remoteIP,
	}
	for _, ep := range endpoints {
		if ep.Func == nil || ep.Rule == "" || ep.Path == "" {
			return nil, errors.New("rulehttp: endpoint needs rule, path and func")
		}
		if have, dup := h.rules[ep.Path]; dup && have.Rule != ep.Rule {
			return nil, errors.New("rulehttp: path " + ep.Path + " bound to both " + have.Rule + " and " + ep.Rule)
		}
		h.rules[ep.Path] = ep
	}
	for _, o := range opts {
		o(h)
	}
	return h, nil
}

// FromSchema is New over a single schema's async rules.
func FromSchema[T any](s *vb.Schema[T], opts ...Option) (*Handler, error) {
	return New(s.Endpoints(), opts...)
}

// Paths returns the served paths, sorted — for logging what is exposed.
func (h *Handler) Paths() []string {
	out := make([]string, 0, len(h.rules))
	for p := range h.rules {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Mount registers every endpoint on mux using method-qualified patterns,
// so a mux serving other routes can host the rules alongside them.
func (h *Handler) Mount(mux *http.ServeMux) {
	for _, p := range h.Paths() {
		mux.Handle(http.MethodPost+" "+p, h)
	}
}

// ServeHTTP implements http.Handler, routing on r.URL.Path.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ep, ok := h.rules[r.URL.Path]
	if !ok {
		h.fail(w, http.StatusNotFound, CodeUnknownRule, "no rule at "+r.URL.Path)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.fail(w, http.StatusMethodNotAllowed, CodeMethod, "use POST")
		return
	}
	if h.authorize != nil {
		if err := h.authorize(r); err != nil {
			h.fail(w, http.StatusForbidden, CodeUnauthorized, err.Error())
			return
		}
	}
	if !h.limiter.Allow(h.clientKey(r)) {
		h.fail(w, http.StatusTooManyRequests, CodeRateLimited, "slow down")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, h.maxBody+1))
	if err != nil {
		h.fail(w, http.StatusBadRequest, CodeBadRequest, "cannot read body")
		return
	}
	if int64(len(body)) > h.maxBody {
		h.fail(w, http.StatusRequestEntityTooLarge, CodeTooLarge, "body exceeds limit")
		return
	}
	var req Request
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		h.fail(w, http.StatusBadRequest, CodeBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Rule != "" && req.Rule != ep.Rule {
		h.fail(w, http.StatusBadRequest, CodeBadRequest, "rule "+req.Rule+" does not match endpoint")
		return
	}
	h.run(w, r.Context(), ep, req.Value)
}

// run executes the rule and writes the validation result.
func (h *Handler) run(w http.ResponseWriter, ctx context.Context, ep vb.Endpoint, value any) {
	err := ep.Func(ctx, value)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, Response{OK: true})
	case isTypeError(err):
		// The client sent a JSON type the rule's Go signature cannot take —
		// a request problem, not a validation verdict.
		h.fail(w, http.StatusBadRequest, CodeBadValue, err.Error())
	default:
		writeJSON(w, http.StatusOK, Response{OK: false, Issues: issuesOf(err, ep.Rule)})
	}
}

func isTypeError(err error) bool {
	var te *vb.TypeError
	return errors.As(err, &te)
}

// issuesOf turns a rule body's error into wire issues: a *verr.Errors is
// passed through, anything else becomes one custom issue in exactly the
// shape the interpreter produces for a failed async rule — Rule is the rule
// KIND, the name travels in params.ref — so a frontend cannot tell whether
// an issue came from the server or from a local executor. Path is left
// empty: the server validates one field and does not know where the client
// keeps it; the runtime fills the field's path in.
func issuesOf(err error, rule string) []verr.Issue {
	var ve *verr.Errors
	if errors.As(err, &ve) && ve.Len() > 0 {
		return ve.Issues
	}
	return []verr.Issue{{Code: verr.CodeCustom, Message: err.Error(), Rule: string(ir.RuleAsync), Params: map[string]any{ir.ParamRef: rule, "error": err.Error()}}}
}

func (h *Handler) fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, Response{OK: false, Error: &ErrorBody{Code: code, Message: msg}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// remoteIP is the default client key: the connection's IP, with the port
// stripped. Proxy headers are ignored on purpose — trusting them without a
// trusted-proxy list lets any client spoof its way past the limiter.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
