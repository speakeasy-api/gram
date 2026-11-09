package constants

const (
	KiB = 1024
	MiB = 1024 * KiB
	GiB = 1024 * MiB

	// MaxRequestBodyBytes bounds every request body that reaches the main
	// middleware chain of a public listener, measured on the wire. The public
	// edge is configured with the same value, so the server refuses what the
	// edge refuses, including chunked uploads an edge that only reads
	// Content-Length cannot measure. Proxies dispatched ahead of that chain
	// (the marketplace and hooks artifact routes) enforce their own limits.
	// Routes that decompress a body may allow more after decompression; this
	// is the ceiling for what arrives.
	MaxRequestBodyBytes = 15 * MiB
)
