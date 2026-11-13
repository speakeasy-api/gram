package guardian

import (
	"errors"
	"net"
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

// IsAddressNotFound reports a lookup that said the name does not exist. A
// temporary or timed-out lookup is not one.
func IsAddressNotFound(err error) bool {
	dnsErr, ok := errors.AsType[*net.DNSError](err)
	return ok && dnsErr.IsNotFound
}
