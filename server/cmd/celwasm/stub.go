//go:build !(js && wasm)

package main

// celwasm is built only for GOOS=js GOARCH=wasm (see main.go); it drives the
// dashboard CEL editor in the browser. This stub keeps the package valid — and
// `go build/vet/fix ./...` happy — on every other platform, where the real entry
// point is excluded by build constraints. Build the real artifact with:
//
//	mise gen:celwasm   # or: GOOS=js GOARCH=wasm go build ./cmd/celwasm
func main() {
	panic("celwasm must be built with GOOS=js GOARCH=wasm")
}
