// Package digitalfx is the module root of the DigitalFX / digitalcapfx backend.
//
// It intentionally contains no runtime code — the runnable entrypoint lives in
// cmd/server. This file exists so tooling that runs `go list ./` at the module
// root (notably swag, which resolves `db.*` types for the OpenAPI spec) can
// determine the root package. Without at least one Go file here, swag fails with
// "no Go files in <root>" and cannot resolve internal type references.
package digitalfx
