package examples

// The proving application, exercised the way a consumer would use it.
//
// Everything else in this module proves a component against an
// independent implementation. This proves the SEAMS: that one set of Go
// rules produces a server and a browser bundle that agree, that the
// artifacts a team would commit are the artifacts the code actually
// produces, and that the loop a user walks — type, see an error, submit,
// see the same error — holds end to end.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ubgo/validator/examples/app/model"
	"github.com/ubgo/validator/interp"
	"github.com/ubgo/validator/jsonschema"
	"github.com/ubgo/validator/verr"
)

func mar(t *testing.T, day int) time.Time {
	t.Helper()
	return time.Date(2026, 3, day, 0, 0, 0, 0, time.UTC)
}

func validBooking() model.Booking {
	return model.Booking{
		Email: "ada@example.com", CheckIn: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		CheckOut: time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC), Guests: 2, Room: "double",
	}
}

// TestAppRulesCoverEveryMechanism walks the application's schema through
// each kind of rule it declares, so a regression in any one of them shows
// up here as a behavioral failure rather than a unit-test failure.
func TestAppRulesCoverEveryMechanism(t *testing.T) {
	v, err := interp.FromSchema(model.Schema)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	cases := []struct {
		name    string
		mutate  func(*model.Booking)
		wantAt  string
		code    string
		because string
	}{
		{"field rule", func(b *model.Booking) { b.Email = "not-an-email" },
			"$.email", "email", "an ordinary field rule, which also crosses to the browser"},
		{"cross-field", func(b *model.Booking) { b.CheckIn, b.CheckOut = mar(t, 10), mar(t, 5) },
			"$.check_out", "after", "two fields compared, which JSON Schema cannot state"},
		{"object rule", func(b *model.Booking) { b.Room, b.Guests = "single", 3 },
			"$", "custom", "reads Room AND Guests, so it belongs to no field"},
		{"object rule, second", func(b *model.Booking) { b.CheckOut = b.CheckIn.AddDate(0, 0, 60) },
			"$", "custom", "a second object rule, told apart by its ref"},
		{"gate", func(b *model.Booking) { b.Room, b.Guests = "suite", 1 },
			"$.note", "required", "a note is required only for this combination"},
		{"enum", func(b *model.Booking) { b.Room = "penthouse" },
			"$.room", "enum", "a closed set"},
		{"bound", func(b *model.Booking) { b.Guests = 9 },
			"$.guests", "max", "a numeric bound"},
		{"collection", func(b *model.Booking) { b.Extras = []string{"breakfast", "breakfast"} },
			"$.extras", "unique", "a rule over a slice"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := validBooking()
			c.mutate(&in)
			errs := v.Run(ctx, &in)
			for _, is := range errs.Issues {
				if is.Path == c.wantAt && is.Code == c.code {
					return
				}
			}
			t.Errorf("no %s issue at %s (%s); got %+v", c.code, c.wantAt, c.because, errs.Issues)
		})
	}

	// The valid booking must pass, or every case above proves nothing.
	if errs := v.Run(ctx, validBooking()); errs.Len() != 0 {
		t.Fatalf("the valid fixture was rejected: %+v", errs.Issues)
	}
}

// TestAppTransformsWriteBack pins that what the handler stores is the
// normalized value, not what arrived.
func TestAppTransformsWriteBack(t *testing.T) {
	v, _ := interp.FromSchema(model.Schema)
	in := validBooking()
	in.Email = "  ADA@Example.COM  "
	if err := v.Validate(context.Background(), &in); err != nil {
		t.Fatal(err)
	}
	if in.Email != "ada@example.com" {
		t.Errorf("email = %q; the transforms did not write back", in.Email)
	}
}

// TestAppErrorsIs is the reason sentinels exist: a handler reacts to a
// kind of failure without comparing strings.
func TestAppErrorsIs(t *testing.T) {
	v, _ := interp.FromSchema(model.Schema)
	in := validBooking()
	in.Room, in.Guests = "suite", 1 // the gate demands a note
	err := v.Validate(context.Background(), &in)

	if !errors.Is(err, verr.ErrRequired) {
		t.Error("errors.Is did not see the required issue")
	}
	if errors.Is(err, verr.ErrType) {
		t.Error("errors.Is matched a code that is not present")
	}
	errs, ok := verr.As(err)
	if !ok {
		t.Fatal("verr.As did not unwrap the result")
	}
	if issue, found := errs.First(verr.ErrRequired); !found || issue.Path != "$.note" {
		t.Errorf("First = %+v", issue)
	}
}

// TestAppExportedArtifactsAreCurrent is the check a team would run in CI:
// the files committed for the frontend must be the files the rules
// produce. A rule changed without re-exporting fails here.
func TestAppExportedArtifactsAreCurrent(t *testing.T) {
	res, err := jsonschema.Emit(model.Schema.IR(), jsonschema.WithID("https://example.com/schemas/booking.json"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := res.JSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("app", "web", "booking.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Error("app/web/booking.schema.json is stale; run `go generate ./app/...`")
	}

	// The IR document the generator reads must be current too.
	irWant, err := json.MarshalIndent(model.Schema.IR(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	irGot, err := os.ReadFile(filepath.Join("app", "model", "booking.ir.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(irGot)) != strings.TrimSpace(string(irWant)) {
		t.Error("app/model/booking.ir.json is stale; run `go generate ./app/...`")
	}
}

// TestAppManifestIsHonest checks the promise the whole design rests on:
// every rule the browser cannot run is named, with a reason.
func TestAppManifestIsHonest(t *testing.T) {
	res, err := jsonschema.Emit(model.Schema.IR())
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[string]jsonschema.Entry{}
	for _, e := range res.Manifest.Entries {
		key := e.Ref
		if key == "" {
			key = e.Name
		}
		byRef[key] = e
	}

	// The async rule, with the endpoint the browser must call.
	if e := byRef["email_free"]; e.Class != jsonschema.ClassServer || e.Endpoint != "/v/email_free" {
		t.Errorf("async entry = %+v", e)
	}
	// Both object rules, told apart by ref, not by name.
	for _, ref := range []string{"fits_the_room", "stay_length"} {
		if e := byRef[ref]; e.Class != jsonschema.ClassOpaque || e.Path != "$" {
			t.Errorf("object rule %s = %+v", ref, e)
		}
	}
	// The transforms.
	for _, name := range []string{"trim", "lower"} {
		if e := byRef[name]; e.Class != jsonschema.ClassLossy {
			t.Errorf("transform %s = %+v", name, e)
		}
	}
	// The cross-field rule, which the browser CAN run, carried as a
	// condition with the message Go would print.
	after := byRef["CheckIn"]
	if after.Cond == nil {
		t.Fatalf("the after rule carries no condition: %+v", after)
	}
	if after.Message != "must be after check_in" {
		t.Errorf("message = %q; it must name the wire field, matching the path", after.Message)
	}
}
