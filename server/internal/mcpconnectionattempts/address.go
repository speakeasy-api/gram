package mcpconnectionattempts

import "github.com/speakeasy-api/gram/server/internal/guardian"

// AddressHappened names a configured address err never dialed; guardian owns the tunnel, redirect and timeout rules.
func AddressHappened(err error, tunneled bool) (Code, bool) {
	return AddressCode(guardian.AddressFailureOf(err, tunneled))
}

// AddressCode is the code for an address failure.
func AddressCode(failure guardian.AddressFailure) (Code, bool) {
	switch failure {
	case guardian.AddressNotAllowed:
		return CodeAddressNotAllowed, true
	case guardian.AddressNotFound:
		return CodeAddressNotFound, true
	case guardian.AddressFine:
	}
	var none Code
	return none, false
}

// AddressOf is the address failure code stands for, AddressFine for any other code.
func AddressOf(code Code) guardian.AddressFailure {
	switch code {
	case CodeAddressNotAllowed:
		return guardian.AddressNotAllowed
	case CodeAddressNotFound:
		return guardian.AddressNotFound
	default:
		return guardian.AddressFine
	}
}
