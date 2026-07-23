// Package wire declares the device-telemetry header names the speakeasy-hooks
// binary stamps on its requests and the server lifts onto hook endpoint spans.
// Both sides import it so the vocabulary cannot drift within the repo;
// deployed binaries still lag the server, so the server treats the values as
// untrusted strings regardless.
package wire

const (
	HeaderDeviceOS             = "X-Gram-Device-Os"
	HeaderDeviceArch           = "X-Gram-Device-Arch"
	HeaderDeviceBinaryVersion  = "X-Gram-Device-Binary-Version"
	HeaderDeviceHarness        = "X-Gram-Device-Harness"
	HeaderDeviceHarnessVariant = "X-Gram-Device-Harness-Variant"
	HeaderDeviceHarnessVersion = "X-Gram-Device-Harness-Version"
	HeaderDeviceElapsedMS      = "X-Gram-Device-Elapsed-Ms"
)
