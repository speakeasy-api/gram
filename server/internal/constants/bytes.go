package constants

const (
	KiB = 1024
	MiB = 1024 * KiB
	GiB = 1024 * MiB

	// MaxRequestBodyBytes bounds every request body a public listener accepts,
	// measured on the wire. The public edge is configured with the same value,
	// so the server refuses what the edge refuses, including chunked uploads an
	// edge that only reads Content-Length cannot measure. Routes that decompress
	// a body may allow more after decompression; this is the ceiling for what
	// arrives.
	MaxRequestBodyBytes = 15 * MiB
)
