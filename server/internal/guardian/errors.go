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
	if timedOut(err) || errors.Is(err, context.DeadlineExceeded) {
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

// timedOut reports a net.Error timeout anywhere in err's tree, joined branches included.
func timedOut(err error) bool {
	if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
		return true
	}
	switch wrapped := err.(type) { //nolint:errorlint // walks the tree itself
	case interface{ Unwrap() error }:
		return wrapped.Unwrap() != nil && timedOut(wrapped.Unwrap())
	case interface{ Unwrap() []error }:
		for _, branch := range wrapped.Unwrap() {
			if branch != nil && timedOut(branch) {
				return true
			}
		}
	}
	return false
}

// Problem says what is wrong with subject's address, for an administrator; empty for AddressFine.
func (f AddressFailure) Problem(subject string) string {
	switch f {
	case AddressNotAllowed:
		return subject + " uses a private or internal address Speakeasy can't reach"
	case AddressNotFound:
		return subject + "'s address doesn't exist"
	case AddressFine:
		return ""
	default:
		return ""
	}
}

// AddressOwner is whose address failed, as an end user reads it.
type AddressOwner string

const (
	// OwnAddress is the server's own address.
	OwnAddress AddressOwner = "its address"

	// SignInAddress is the address of the server's sign-in.
	SignInAddress AddressOwner = "its sign-in address"

	// ThisSignInAddress is the address of the sign-in a connect page card shows.
	ThisSignInAddress AddressOwner = "this sign-in's address"
)

// EndUserMessage tells an end user in everyday words what failed, that waiting won't help, and what to ask for.
func (f AddressFailure) EndUserMessage(subject string, owner AddressOwner) string {
	ask := "fix the server's address"
	if owner != OwnAddress {
		ask = "fix the server's sign-in address"
	}
	cause := string(owner) + " doesn't exist"
	if f == AddressNotAllowed {
		cause = string(owner) + " is a private or internal one that Speakeasy can't reach"
	}
	return subject + " can't connect: " + cause + ". Waiting won't fix this. Ask your administrator to " + ask + "."
}

// cardSubjects names each owner's address at the start of a card sentence.
var cardSubjects = map[AddressOwner]string{
	OwnAddress:        "This server's address",
	SignInAddress:     "This server's sign-in address",
	ThisSignInAddress: "This sign-in's address",
}

// CardStatus is the connect page's status line for f: what is wrong, then what to do.
func (f AddressFailure) CardStatus(owner AddressOwner) string {
	subject := cardSubjects[owner]
	problem := subject + " doesn't exist."
	if f == AddressNotAllowed {
		problem = subject + " is private or internal, so Speakeasy can't reach it."
	}
	return "Can't connect · " + problem + " Ask your administrator to fix it."
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
