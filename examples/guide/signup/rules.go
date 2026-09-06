package signup

import (
	"context"
	"errors"

	"github.com/ubgo/validator/vb"
)

// reserved is what the not_reserved custom rule consults. In a real
// service this is a table; here it is enough to show the rule shape.
var reserved = map[string]bool{"admin@example.com": true, "root@example.com": true}

// Schema holds every rule for Signup. It is built once at package init and
// checked then: an invalid regex, an unknown rule or two functions
// registered under one name panic at startup, never on a request.
var Schema = vb.Struct("Signup",
	vb.F(SignupFields.Email, vb.String().Email().
		// Go-only rule: runs on the server, the browser is told it exists.
		Custom("not_reserved", func(_ context.Context, email string) error {
			if reserved[email] {
				return errors.New("reserved address")
			}
			return nil
		}).
		// Server-backed rule: Go runs it; the browser POSTs to /v/email_free.
		Async("email_free", "/v/email_free", func(_ context.Context, email string) error {
			if email == "taken@example.com" {
				return errors.New("already registered")
			}
			return nil
		})).Trim().Lower(),
	vb.F(SignupFields.Password, vb.String().MinLength(8).MaxLength(72)),
	vb.F(SignupFields.Age, vb.Int[int]().Min(18).Max(120)),
	vb.F(SignupFields.Plan, vb.String().Enum("free", "pro", "team")),
	vb.F(SignupFields.Website, vb.Ptr(vb.String().URL())).Optional(),
	vb.F(SignupFields.Tags, vb.Array(vb.String().MinLength(1)).MaxItems(5).Unique()).Optional(),
	vb.F(SignupFields.Starts, vb.Time()),
	vb.F(SignupFields.Ends, vb.Time().With(vb.After(SignupFields.Starts))),
	vb.F(SignupFields.Company, vb.String()).Optional(),
	vb.F(SignupFields.VATID, vb.String().Pattern(`^[A-Z]{2}[0-9A-Z]{8,12}$`)).Optional(),
).Gate(
	// A team plan needs a company name.
	vb.Requires(vb.Eq(SignupFields.Plan, "team"), SignupFields.Company),
	// If a VAT id is given, the company must be given too.
	vb.Requires(vb.Has(SignupFields.VATID), SignupFields.Company),
).MustBuild()
