package guardian

import (
	"context"
	"errors"
	"net"
	"net/http"
	"syscall"
)

// IsDeadPeerDialError reports connect failures that definitively identify a
// dead peer.
func IsDeadPeerDialError(err error) bool {
	opErr, ok := errors.AsType[*net.OpError](err)
	if !ok || opErr.Op != "dial" {
		return false
	}

	return opErr.Timeout() ||
		errors.Is(opErr, syscall.ECONNREFUSED) ||
		errors.Is(opErr, syscall.EHOSTUNREACH) ||
		errors.Is(opErr, syscall.ENETUNREACH)
}

// AddressFailure is a configured address Speakeasy did not dial.
type AddressFailure string

const (
	// AddressFine is any other outcome, including no failure at all.
	AddressFine AddressFailure = ""

	// AddressNotAllowed is an address the egress policy refuses.
	AddressNotAllowed AddressFailure = "not_allowed"

	// AddressNotFound is a host name that does not exist.
	AddressNotFound AddressFailure = "not_found"
)

// AddressFailureOf names a configured address err never dialed; tunnel dials, redirect hops and timeouts never are.
func AddressFailureOf(err error, tunneled bool) AddressFailure {
	if err == nil || tunneled {
		return AddressFine
	}
	if _, hop := errors.AsType[*redirectHopError](err); hop {
		return AddressFine
	}
	// A timeout anywhere in the chain wins: the call also ran out of time.
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return AddressFine
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return AddressFine
	}
	if errors.Is(err, ErrBlockedIP) {
		return AddressNotAllowed
	}
	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok && dnsErr.IsNotFound {
		return AddressNotFound
	}
	return AddressFine
}

// Problem completes a sentence about the thing at the address, empty for AddressFine.
func (f AddressFailure) Problem() string {
	switch f {
	case AddressNotAllowed:
		return "uses a private or internal address Speakeasy can't reach"
	case AddressNotFound:
		return "uses an address that does not point anywhere"
	case AddressFine:
		return ""
	default:
		return ""
	}
}

// redirectHopError is a dial to an address an upstream redirected to, not one configured here.
type redirectHopError struct{ err error }

func (e *redirectHopError) Error() string { return e.err.Error() }
func (e *redirectHopError) Unwrap() error { return e.err }

// redirectHopMarker marks address failures on redirect hops, so they keep their usual classification.
type redirectHopMarker struct{ next http.RoundTripper }

func (m redirectHopMarker) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := m.next.RoundTrip(req)
	if err != nil && req.Response != nil && AddressFailureOf(err, false) != AddressFine {
		return resp, &redirectHopError{err: err}
	}
	return resp, err //nolint:wrapcheck // retry policies type-assert transport errors, so they pass through unchanged
}
