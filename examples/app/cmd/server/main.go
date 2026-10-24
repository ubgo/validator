// Command server is the proving application: a real HTTP service built
// with this library the way a consumer would build one.
//
//	go run ./cmd/server -addr :8080
//	curl -s localhost:8080/api/bookings -d '{"email":"taken@example.com","guests":4,"room":"single"}' | jq
//
// It exercises, in one process, everything the library offers a Go user:
// the interpreter for the request body, the async rule endpoints the
// browser round-trips to, message translation, errors.Is for reacting to
// a specific failure, and the static frontend that validates with the
// exported schema before the request is ever sent.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ubgo/validator/examples/app/model"
	"github.com/ubgo/validator/i18n"
	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/rulehttp"
	"github.com/ubgo/validator/verr"
)

const (
	// maxBody bounds the request body: the decoder should never be handed
	// an unbounded stream.
	maxBody = 1 << 20

	// readHeaderTimeout is set because http.Server's zero value has none,
	// which is a slow-loris footgun rather than a validation concern.
	readHeaderTimeout = 5 * time.Second
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	// Compiled once. FromSchema resolves every custom and async name and
	// compiles every regex, so a schema this process cannot execute is a
	// startup failure rather than a surprise on some later request.
	validate, err := interp.FromSchema(model.Schema)
	if err != nil {
		log.Fatalf("the schema cannot be executed: %v", err)
	}

	// The async rules, served at the endpoints the manifest advertises.
	// The browser calls these while the user types; this handler and the
	// validator above run the very same function.
	rules, err := rulehttp.FromSchema(model.Schema)
	if err != nil {
		log.Fatal(err)
	}

	// One catalog, shared with the frontend as JSON, so a German user sees
	// the same wording from the browser and from the server.
	catalogs := map[string]i18n.Catalog{"en": i18n.Default()}
	if de, err := loadCatalog("web/de.json"); err == nil {
		catalogs["de"] = i18n.Merge(i18n.Default(), de)
	}

	mux := http.NewServeMux()
	rules.Mount(mux)
	mux.Handle("GET /", http.FileServer(http.Dir("web")))
	mux.HandleFunc("POST /api/bookings", func(w http.ResponseWriter, r *http.Request) {
		var in model.Booking
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&in); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed JSON: " + err.Error()})
			return
		}

		// Validate. Transforms rewrite the value in place, so what is
		// stored below is the normalized form, not what arrived.
		err := validate.Validate(r.Context(), &in)

		// errors.Is answers "did this fail for a particular reason"
		// without reaching into the issue list and comparing strings.
		if errors.Is(err, verr.Code("async")) || errors.Is(err, verr.ErrCustom) {
			log.Printf("booking rejected by a server-side rule: %v", err)
		}

		if errs, ok := verr.As(err); ok {
			// Render in the caller's language. The codes are the same
			// either way, so the frontend keys off code, not text.
			if c, found := catalogs[lang(r)]; found {
				errs = c.Localize(errs)
			}
			writeJSON(w, http.StatusUnprocessableEntity, errs)
			return
		}

		writeJSON(w, http.StatusCreated, map[string]any{
			"status": "confirmed",
			"email":  in.Email, // trimmed and lower-cased by the transforms
			"nights": int(in.CheckOut.Sub(in.CheckIn).Hours() / 24),
		})
	})

	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: readHeaderTimeout}
	log.Printf("listening on %s — open http://localhost%s", *addr, *addr)
	log.Fatal(srv.ListenAndServe())
}

// lang reads a language preference, honouring only what we have a catalog
// for. A real service would parse Accept-Language properly.
func lang(r *http.Request) string {
	if q := r.URL.Query().Get("lang"); q != "" {
		return q
	}
	if a := r.Header.Get("Accept-Language"); strings.HasPrefix(a, "de") {
		return "de"
	}
	return "en"
}

func loadCatalog(path string) (i18n.Catalog, error) {
	f, err := os.Open(path)
	if err != nil {
		return i18n.Catalog{}, err
	}
	defer f.Close()
	return i18n.Load(f)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
