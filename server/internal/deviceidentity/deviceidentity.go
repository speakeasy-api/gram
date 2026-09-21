// Package deviceidentity normalizes the hardware identity a Speakeasy device
// agent reports on its requests through the Gram-Device-* headers: the
// machine's serial number, its hostname, and what kind of machine it is.
//
// Both the agent service, which stores these values, and the HTTP request
// logger, which records them on the request's wide event, normalize through
// here. Sharing one implementation is the point: a device count taken from
// the request logs and a count of the stored per-device heartbeats have to
// agree, and they only do if casing and the placeholder-serial set collapse
// identically on both paths.
package deviceidentity

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// Headers the device agent reports its machine's identity in. Every one is
// omitted when the agent cannot read the value, so an absent header is
// ordinary and means "unknown" — never "empty".
const (
	// HeaderSerial carries the machine's hardware serial number.
	HeaderSerial = "Gram-Device-Serial"

	// HeaderHostname carries the machine's hostname.
	HeaderHostname = "Gram-Device-Hostname"

	// HeaderEnvironment carries what kind of machine the agent runs on, as
	// one of the Environment* values. An absent header means
	// EnvironmentEndpoint.
	HeaderEnvironment = "Gram-Device-Environment"
)

// Environment kinds an agent may declare.
//
// EnvironmentEndpoint — an end-user device of any form factor, named so it
// does not expire the way "laptop" would on a desktop or a phone — is the
// default, and is the one kind whose heartbeat is recorded as an ordinary
// per-user sync rather than as an environment sync. It is still a real value
// rather than an absence, so NormalizeEnvironment returns it and call sites
// compare against it by name.
const (
	EnvironmentEndpoint  = "endpoint"
	EnvironmentEphemeral = "ephemeral"
	EnvironmentServer    = "server"
)

// placeholderSerials are SMBIOS/DMI defaults that white-box hardware reports
// verbatim instead of a real serial. Every MDM passes them straight through,
// so an organization can hold many DIFFERENT machines carrying the identical
// "serial" in inventory. Storing a heartbeat under one would let a single
// agent install attest every one of those machines as device-verified — the
// strongest claim this product makes, asserted for machines that never ran
// the agent. Counting them would be wrong in the opposite direction: an
// entire fleet of white-box PCs would read as one device.
//
// Rejecting them costs those devices nothing: they fall back to the assigned
// user's email match, exactly like an agent that reports no serial at all.
var placeholderSerials = map[string]bool{
	"to be filled by o.e.m.": true,
	"to be filled by oem":    true,
	"default string":         true,
	"system serial number":   true,
	"not specified":          true,
	"not applicable":         true,
	"unknown":                true,
	"none":                   true,
	"n/a":                    true,
	"invalid":                true,
	"0":                      true,
	"123456789":              true,
	"0123456789":             true,
	"serial number":          true,
	"oem":                    true,
	"o.e.m.":                 true,
}

// NormalizeSerial canonicalizes an agent-reported hardware serial, returning
// "" when the value cannot serve as a device identity.
//
// Lowercasing mirrors conv.NormalizeEmail on the sibling user path: the
// per-device heartbeat table's dedup key and every coverage reader compare
// LOWER(serial_number), so the value must already be in that form or one
// machine could hold two rows and fan out its coverage. The same holds for a
// device count taken over logs, where two casings of one serial would count
// as two machines.
func NormalizeSerial(reported *string) string {
	serial := strings.ToLower(strings.TrimSpace(conv.PtrValOr(reported, "")))
	if placeholderSerials[serial] {
		return ""
	}
	return serial
}

// NormalizeEnvironment maps the declared kind onto the closed set. Total:
// every input resolves to one of the Environment* values, never to "".
//
// Three things collapse onto EnvironmentEndpoint, and they are the same thing
// as far as this server is concerned — "an ordinary device, recorded the way
// it always was":
//
//   - an explicit "endpoint"
//   - an absent header, which every agent predating this field sends
//   - an unrecognized value
//
// The last of those degrades rather than erroring, deliberately. Rejecting
// the poll would stop that device syncing plugins at all — an outage caused
// by an attribution hint. A newer agent inventing a kind this server has not
// heard of keeps working, and lands where it would have landed anyway.
func NormalizeEnvironment(reported *string) string {
	switch strings.ToLower(strings.TrimSpace(conv.PtrValOr(reported, ""))) {
	case EnvironmentEphemeral:
		return EnvironmentEphemeral
	case EnvironmentServer:
		return EnvironmentServer
	case EnvironmentEndpoint:
		// Listed rather than folded into the default so the closed set is
		// visibly exhaustive here, and so the constant is not a declaration
		// nothing reads.
		return EnvironmentEndpoint
	default:
		return EnvironmentEndpoint
	}
}
