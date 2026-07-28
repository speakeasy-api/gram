// Package dsl is a minimal stub of goa.design/goa/v3/dsl exposing just the
// HTTP route functions the rpc-endpoint-format analyzer inspects.
package dsl

func GET(path string) {}

func HEAD(path string) {}

func POST(path string) {}

func PUT(path string) {}

func DELETE(path string) {}

func CONNECT(path string) {}

func OPTIONS(path string) {}

func TRACE(path string) {}

func PATCH(path string) {}

// Path is an unrelated DSL function that also takes a string, used to confirm
// the analyzer keys off the specific route functions rather than any string arg.
func Path(path string) {}
