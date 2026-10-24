// Command signup-server is the guide's complete HTTP example: decode a JSON
// body into the struct, validate it, answer 422 with the issues, and serve
// the async rules the browser round-trips to.
//
//	go run ./guide/cmd/signup-server -addr :8080
//	curl -s -X POST localhost:8080/signup -d '{"email":" Ada@Example.com ","password":"short","age":17,"plan":"team","starts":"2026-01-02T00:00:00Z","ends":"2026-01-01T00:00:00Z"}'
//	curl -s -X POST localhost:8080/v/email_free -d '{"rule":"email_free","value":"taken@example.com"}'
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"

	"github.com/ubgo/validator/examples/guide/signup"
	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/rulehttp"
	"github.com/ubgo/validator/verr"
)

// maxBody bounds the request body so a client cannot make the decoder
// allocate without limit.
const maxBody = 1 << 20

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	// Compile the schema once. FromSchema checks the IR, compiles every
	// regex and resolves every custom/async name against the schema's
	// registry; anything it cannot execute is an error here, not later.
	validate, err := interp.FromSchema(signup.Schema)
	if err != nil {
		log.Fatal(err)
	}

	// The async rules, served at their declared endpoints (POST /v/email_free).
	rules, err := rulehttp.FromSchema(signup.Schema)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/v/", rules)
	mux.HandleFunc("POST /signup", func(w http.ResponseWriter, r *http.Request) {
		var in signup.Signup
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&in); err != nil {
			http.Error(w, "malformed JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Validate the decoded struct. Transforms (Trim, Lower on email)
		// write back into `in`, so what was validated is what gets stored.
		err := validate.Validate(r.Context(), &in)
		var errs *verr.Errors
		if errors.As(err, &errs) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			// Body: {"issues":[{"code":"min_length","path":"$.password","message":"must be at least 8 characters","rule":"min_length","params":{"value":8}}, ...]}
			_ = json.NewEncoder(w).Encode(errs)
			return
		}

		// Valid: in.Email is now trimmed and lowercased.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "created", "email": in.Email})
	})

	log.Println("listening on", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
