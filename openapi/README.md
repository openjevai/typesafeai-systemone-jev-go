# TypeSafe OpenAPI specification

`typesafe-openapi.json` is the OpenAPI 3.1 description of the TypeSafe API,
fetched from <https://api.typesafe.ai/openapi.json>.

- Spec version: 0.2.0
- Fetched: 2026-09-19
- Endpoints: `POST /v1/systemone`, `GET /v1/models`

The Go types in this repository are hand-written to match this specification,
and `spec_test.go` checks them against the schema examples in the file. When
TypeSafe publishes a new spec version, replace the file, run `go test ./...`,
and adjust any drift.

The specification is TypeSafe's; it is vendored here as a reference for the
client implementation.
