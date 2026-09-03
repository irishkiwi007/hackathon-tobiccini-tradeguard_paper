module github.com/yourusername/tradeguard

go 1.25.5

require (
	github.com/alpacahq/alpaca-trade-api-go/v3 v3.11.0
	github.com/joho/godotenv v1.5.1
	github.com/mark3labs/mcp-go v0.58.0
	github.com/shopspring/decimal v1.4.0
)

require (
	cloud.google.com/go v0.118.0 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/text v0.21.0 // indirect
)

// After cloning, run `go mod tidy` on a machine with normal internet
// access to resolve exact versions and generate go.sum — go.sum is not
// included in this scaffold.
//
// Version pins above were checked against real git tags (not guessed):
// v3.11.0 is confirmed the latest alpaca-trade-api-go/v3 tag (an
// earlier draft of this file had v3.20.0, which does not exist — fixed
// after it caused a `go mod tidy` failure), v1.4.0 the latest
// shopspring/decimal, v1.5.1 the latest stable joho/godotenv (newer
// v1.6.0-pre.* tags are pre-releases), and v0.58.0 a confirmed real
// mark3labs/mcp-go tag. `go mod tidy` may bump these further if newer
// versions ship before you run it — that's expected and fine.
