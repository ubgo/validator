// The CEL engine for ubgo/validator, kept in its own module on purpose.
//
// The root module has no third-party dependencies and that is a property
// worth protecting: anyone can depend on the validator without inheriting
// a dependency tree. cel-go is not small, and most schemas never use an
// expression, so it lives here and is registered explicitly by callers who
// want it.
module github.com/ubgo/validator/cel

go 1.24

require github.com/ubgo/validator v0.0.0

require (
	cel.dev/cel-go v0.32.0
	cel.dev/expr v0.25.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20240823005443-9b4947da3948 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240826202546-f6391c0de4c7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240826202546-f6391c0de4c7 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)

replace github.com/ubgo/validator => ../
