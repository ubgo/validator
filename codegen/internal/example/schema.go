package example

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ubgo/validator/ir"
	"github.com/ubgo/validator/vb"
)

// Field refs written by hand here; a real project would run
// `validatorctl fields` and use the generated OrderFields. validate.gen.go
// is refreshed by `UPDATE_GOLDEN=1 go test ./codegen`, which is the
// generator's own golden test, so the fixture and the generator cannot
// drift apart.
var (
	fStreet   = vb.NewField("Street", func(a Address) string { return a.Street }).JSON("street")
	fZip      = vb.NewField("Zip", func(a Address) string { return a.Zip }).JSON("zip")
	fLineSku  = vb.NewField("Sku", func(l Line) string { return l.Sku }).JSON("sku")
	fLineQty  = vb.NewField("Qty", func(l Line) int { return l.Qty }).JSON("qty")
	fEmail    = vb.NewField("Email", func(o Order) string { return o.Email }).JSON("email")
	fQty      = vb.NewField("Qty", func(o Order) int { return o.Qty }).JSON("qty")
	fWeight   = vb.NewField("Weight", func(o Order) float64 { return o.Weight }).JSON("weight")
	fRush     = vb.NewField("Rush", func(o Order) bool { return o.Rush }).JSON("rush")
	fNote     = vb.NewField("Note", func(o Order) *string { return o.Note }).JSON("note")
	fPlaced   = vb.NewField("Placed", func(o Order) time.Time { return o.Placed }).JSON("placed")
	fShip     = vb.NewField("Ship", func(o Order) *time.Time { return o.Ship }).JSON("ship")
	fWindow   = vb.NewField("Window", func(o Order) time.Duration { return o.Window }).JSON("window")
	fTags     = vb.NewField("Tags", func(o Order) []string { return o.Tags }).JSON("tags")
	fScores   = vb.NewField("Scores", func(o Order) []int { return o.Scores }).JSON("scores")
	fMeta     = vb.NewField("Meta", func(o Order) map[string]string { return o.Meta }).JSON("meta")
	fBilling  = vb.NewField("Billing", func(o Order) Address { return o.Billing }).JSON("billing")
	fShipping = vb.NewField("Shipping", func(o Order) *Address { return o.Shipping }).JSON("shipping")
	fLines    = vb.NewField("Lines", func(o Order) []Line { return o.Lines }).JSON("lines")
	fRaw      = vb.NewField("Raw", func(o Order) []byte { return o.Raw }).JSON("raw")
	fExtra    = vb.NewField("Extra", func(o Order) any { return o.Extra }).JSON("extra")
	fCoupon   = vb.NewField("Coupon", func(o Order) string { return o.Coupon }).JSON("coupon")
	fType     = vb.NewField("Type", func(o Order) string { return o.Type }).JSON("type")
	fURL      = vb.NewField("URL", func(o Order) string { return o.URL }).JSON("url")
	fVenue    = vb.NewField("Venue", func(o Order) string { return o.Venue }).JSON("venue")
	fSku      = vb.NewField("Sku", func(o Order) string { return o.Sku }).JSON("sku")
	fNick     = vb.NewField("Nick", func(o Order) string { return o.Nick }).JSON("nick")
	fRef      = vb.NewField("Ref", func(o Order) any { return o.Ref }).JSON("ref")
	fEvent    = vb.NewField("Event", func(o Order) Event { return o.Event }).JSON("event")
	fPref     = vb.NewField("Pref", func(o Order) *string { return o.Pref }).JSON("pref")
	fHost     = vb.NewField("Host", func(o Order) string { return o.Host }).JSON("host")
	fVer      = vb.NewField("Ver", func(o Order) string { return o.Ver }).JSON("ver")
	fAddr     = vb.NewField("Addr", func(o Order) string { return o.Addr }).JSON("addr")
	fChoice   = vb.NewField("Choice", func(o Order) any { return o.Choice }).JSON("choice")
	fCode     = vb.NewField("Code", func(o Order) string { return o.Code }).JSON("code")
	fBag      = vb.NewField("Bag", func(o Order) any { return o.Bag }).JSON("bag")
	fContact  = vb.NewField("Contact", func(o Order) string { return o.Contact }).JSON("contact")
	fUnits    = vb.NewField("Units", func(o Order) int { return o.Units }).JSON("units")
	fCard     = vb.NewField("Card", func(o Order) string { return o.Card }).JSON("card")
	fZone     = vb.NewField("Zone", func(o Order) string { return o.Zone }).JSON("zone")
	fBook     = vb.NewField("Book", func(o Order) string { return o.Book }).JSON("book")
	fBank     = vb.NewField("Bank", func(o Order) string { return o.Bank }).JSON("bank")
	fOID      = vb.NewField("OID", func(o Order) string { return o.OID }).JSON("oid")
	fToken    = vb.NewField("Token", func(o Order) string { return o.Token }).JSON("token")
	fLat      = vb.NewField("Lat", func(o Order) float64 { return o.Lat }).JSON("lat")

	fEvKind = vb.NewField("Kind", func(e Event) string { return e.Kind }).JSON("kind")
	fEvURL  = vb.NewField("URL", func(e Event) string { return e.URL }).JSON("url")
	fEvAddr = vb.NewField("Address", func(e Event) *Address { return e.Address }).JSON("address")
)

// AddressSchema is referenced twice (value and pointer) so it lands in Defs.
var AddressSchema = vb.Struct("Address",
	vb.F(fStreet, vb.String().MinLength(1)),
	vb.F(fZip, vb.String().Pattern(`^[0-9]{5}$`)).Optional(),
).MustBuild()

// OrderSchema is the fixture: one of everything the generator handles.
var OrderSchema = vb.Struct("Order",
	vb.F(fEmail, vb.String().Email().
		Custom("no_temp", func(_ context.Context, s string) error {
			if strings.HasSuffix(s, "@temp.com") {
				return errors.New("temp domains are not allowed")
			}
			return nil
		}).
		Async("unique_email", "/v/unique_email", func(_ context.Context, s string) error {
			if s == "taken@example.com" {
				return errors.New("already taken")
			}
			return nil
		})),
	vb.F(fQty, vb.Int[int]().Min(1).Max(100).MultipleOf(1).Msg("qty_step", "whole units only")),
	vb.F(fWeight, vb.Float[float64]().Gt(0).Lte(50)),
	vb.F(fRush, vb.Bool()),
	vb.F(fNote, vb.Ptr(vb.String().MinLength(2).MaxLength(10))).Optional(),
	vb.F(fPlaced, vb.Time()),
	vb.F(fShip, vb.Ptr(vb.Time().With(vb.After(fPlaced)))).Optional(),
	vb.F(fWindow, vb.Duration().Gt(0).Lt(24*time.Hour)),
	vb.F(fTags, vb.Array(vb.String().Enum("a", "b", "c")).MinItems(1).MaxItems(3).Unique()),
	vb.F(fScores, vb.Array(vb.Int[int]().Gte(0).Lte(10)).Unique()).Optional(),
	vb.F(fMeta, vb.Map(vb.String().MaxLength(3), vb.String().MinLength(1)).MaxItems(2)).Optional(),
	vb.F(fBilling, vb.Ref(AddressSchema)),
	vb.F(fShipping, vb.Ptr(vb.Ref(AddressSchema))).Optional(),
	vb.F(fLines, vb.Array(vb.Object(
		vb.F(fLineSku, vb.String().UUID()),
		vb.F(fLineQty, vb.Int[int]().Gt(0)),
	)).MinItems(1)),
	vb.F(fRaw, vb.Bytes().MaxLength(4)).Optional(),
	vb.F(fExtra, vb.Any[any]()).Optional(),
	vb.F(fCoupon, vb.String().ULID()).Optional(),
	vb.F(fType, vb.String().Enum("online", "offline")),
	vb.F(fURL, vb.String().URL()).Optional(),
	vb.F(fVenue, vb.String()).Optional(),
	vb.F(fSku, vb.String().Const("SKU")).Optional(),
	vb.F(fNick, vb.String().MinLength(2).MaxLength(8)).Trim().Lower().Default("anon"),
	vb.F(fRef, vb.Union[any](vb.Int[int]().Min(1), vb.String().UUID(), vb.Ref(AddressSchema))).Optional(),
	vb.F(fEvent, vb.OneOf[Event]("kind").
		Case("online", vb.Object(
			vb.F(fEvKind, vb.String().Const("online")),
			vb.F(fEvURL, vb.String().URL()),
		)).
		Case("offline", vb.Object(
			vb.F(fEvKind, vb.String().Const("offline")),
			vb.F(fEvAddr, vb.Ptr(vb.Ref(AddressSchema))),
		))),
	vb.F(fPref, vb.Ptr(vb.String().Enum("A", "B"))).Upper().Default("a").Optional(),
	vb.F(fHost, vb.String().Hostname().NotBlank()).Optional(),
	vb.F(fVer, vb.String().Semver()).Optional(),
	vb.F(fAddr, vb.String().IP().MAC()).Optional(),
	// Interface-typed discriminated union: delegated to the interpreter.
	vb.F(fChoice, vb.OneOf[any]("kind").
		Case("a", vb.Object(vb.F(fEvKind, vb.String().Const("a")), vb.F(fEvURL, vb.String().URL()))).
		Case("b", vb.Object(vb.F(fEvKind, vb.String().Const("b"))))).Optional(),
	// Union on a concrete string field: typed probes, no type switch.
	vb.F(fCode, vb.Union[string](vb.String().UUID(), vb.String().MinLength(8).Pattern("^SKU-"), vb.Int[int]())).Optional(),
	// Union with an array variant: delegated to the interpreter.
	vb.F(fBag, vb.Union[any](vb.String().MinLength(2), vb.Array(vb.Int[int]().Min(1)).MinItems(1))).Optional(),
	vb.F(fContact, vb.String().Email()).NormalizeEmail().Optional(),
	vb.F(fUnits, vb.Int[int]().Min(1)).Coerce().Optional(),
	vb.F(fCard, vb.String().CreditCard()).Optional(),
	// The long-tail formats, so both executors are held to the same answer
	// on them: a checksum, a database lookup, and three grammars.
	vb.F(fZone, vb.String().Timezone()).Optional(),
	vb.F(fBook, vb.String().ISBN()).Optional(),
	vb.F(fBank, vb.String().BIC()).Optional(),
	vb.F(fOID, vb.String().MongoID()).Optional(),
	vb.F(fToken, vb.String().Base64URL()).Optional(),
	vb.F(fLat, vb.Float[float64]().Latitude()),
).
	// An object-level rule: its subject is the whole order, not a field.
	// No gate can express this, because a gate decides whether other rules
	// apply and reports nothing itself.
	Custom("order_consistent", func(_ context.Context, o Order) error {
		if o.Qty > 50 && o.Type == "offline" {
			return errors.New("bulk orders must be online")
		}
		return nil
	}).
	Extra(ir.ExtraForbid).
	Gate(
		vb.When(vb.Eq(fType, "online"), []ir.TargetRule{vb.On(fURL, ir.MinLength(1))}, []ir.TargetRule{vb.On(fVenue, ir.MinLength(1))}),
		vb.Requires(ir.And(vb.Has(fCoupon), ir.Not(vb.In(fType, "offline"))), fVenue),
		vb.Group(ir.AtLeastOne, fURL, fVenue),
		vb.When(ir.Or(vb.FieldCmp(fWeight, ir.OpGt, fWeight), vb.Cmp(fQty, ir.OpGte, 50)), []ir.TargetRule{vb.On(fRush, ir.Const(true))}, nil),
		vb.When(ir.CustomCond("vip"), nil, []ir.TargetRule{vb.On(fQty, ir.Lte(10))}),
	).
	MustBuild()

// Registry serves the generated code's custom rules and conditions.
type Registry struct{}

// errNotVIP is the predicate's false. A sentinel, because a condition
// evaluated false on every non-VIP order must not allocate an error each
// time.
var errNotVIP = errors.New("not vip")

// vip is the gate predicate, typed on Order so the generated code's
// rt.CondT reaches it without boxing.
func vip(_ context.Context, o Order) error {
	if strings.HasSuffix(o.Email, "@vip.example.com") {
		return nil
	}
	return errNotVIP
}

// Func implements rt.Registry and interp.Funcs by reading OrderSchema's
// registry and adding the "vip" predicate the gate names directly.
func (Registry) Func(name string) (vb.Func, bool) {
	if name == "vip" {
		return func(ctx context.Context, v any) error {
			if o, ok := v.(Order); ok {
				return vip(ctx, o)
			}
			return errNotVIP
		}, true
	}
	return OrderSchema.Func(name)
}

// TypedFunc implements rt.TypedRegistry, the allocation-free path.
func (Registry) TypedFunc(name string) (any, bool) {
	if name == "vip" {
		return vip, true
	}
	return OrderSchema.TypedFunc(name)
}
