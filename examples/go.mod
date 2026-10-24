module github.com/ubgo/validator/examples

go 1.25.0

require (
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2
	github.com/ubgo/validator v0.0.0
)

require golang.org/x/text v0.41.0 // indirect

replace github.com/ubgo/validator => ../

replace github.com/ubgo/validator/cel => ../cel
