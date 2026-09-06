package benchmodel

import "github.com/ubgo/validator/vb"

// Schema states the same rules as the validate tags on BenchUser.
var Schema = vb.Struct("BenchUser",
	vb.F(BenchUserFields.Email, vb.String().Email()),
	vb.F(BenchUserFields.Name, vb.String().MinLength(2).MaxLength(40)),
	vb.F(BenchUserFields.Age, vb.Int[int]().Gte(18).Lte(120)),
	vb.F(BenchUserFields.Plan, vb.String().Enum("free", "pro", "team")),
	vb.F(BenchUserFields.Website, vb.String().URL()).Optional(),
	vb.F(BenchUserFields.UUID, vb.String().UUID()),
	vb.F(BenchUserFields.Tags, vb.Array(vb.String().MinLength(1)).MaxItems(5)).Optional(),
	vb.F(BenchUserFields.Score, vb.Float[float64]().Gte(0).Lte(100)),
	vb.F(BenchUserFields.Country, vb.String().CountryCode()),
	vb.F(BenchUserFields.Created, vb.Time()),
	vb.F(BenchUserFields.Nickname, vb.String().MinLength(2).MaxLength(16)).Optional(),
).MustBuild()
