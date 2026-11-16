package guardian_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestIsDeadPeerDialErrorRecognizesDefinitiveConnectFailures(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{
		syscall.ECONNREFUSED,
		syscall.EHOSTUNREACH,
		syscall.ENETUNREACH,
		&net.DNSError{IsTimeout: true},
	} {
		dialErr := &net.OpError{Op: "dial", Net: "tcp", Err: cause}
		transportErr := &url.Error{Op: "Post", URL: "http://10.0.0.1:8091", Err: dialErr}
		err := fmt.Errorf("proxy post: %w", oops.E(oops.CodeGatewayError, transportErr, "remote mcp server unreachable"))

		require.True(t, guardian.IsDeadPeerDialError(err), "cause %T %v", cause, cause)
	}
}

func TestIsDeadPeerDialErrorRejectsNonDeadPeerFailures(t *testing.T) {
	t.Parallel()

	errs := []error{
		&url.Error{Op: "Post", URL: "http://10.0.0.1:8091", Err: &net.OpError{Op: "dial", Net: "tcp", Err: guardian.ErrBlockedIP}},
		&url.Error{Op: "Post", URL: "https://10.0.0.1:8091", Err: errors.New("tls: failed to verify certificate")},
		&url.Error{Op: "Post", URL: "http://10.0.0.1:8091", Err: &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}},
		context.Canceled,
	}

	for _, err := range errs {
		require.False(t, guardian.IsDeadPeerDialError(err), "error %T %v", err, err)
	}
}

// asNetError exposes a net.Error only through As, not by its own type.
type asNetError struct{ timeout bool }

func (e asNetError) Error() string { return "network error" }

func (e asNetError) As(target any) bool {
	netErr, ok := target.(*net.Error)
	if ok {
		*netErr = &net.DNSError{Err: "network error", Name: "a.example.com", IsTimeout: e.timeout}
	}
	return ok
}

// A timeout anywhere in a joined chain wins over a refused or missing address, in either order.
func TestAddressFailureOf_TimeoutAnywhereWins(t *testing.T) {
	t.Parallel()

	blocked := &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("192.0.2.10: %w", guardian.ErrBlockedIP)}
	missing := &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "a.example.invalid", IsNotFound: true}}
	timeout := &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "timeout", Name: "a.example.com", IsTimeout: true}}

	for name, tc := range map[string]struct {
		err  error
		want guardian.AddressFailure
	}{
		"blocked alone":                         {err: blocked, want: guardian.AddressNotAllowed},
		"missing alone":                         {err: missing, want: guardian.AddressNotFound},
		"blocked then timeout":                  {err: errors.Join(blocked, timeout), want: guardian.AddressFine},
		"timeout then blocked":                  {err: errors.Join(timeout, blocked), want: guardian.AddressFine},
		"missing then timeout":                  {err: errors.Join(missing, fmt.Errorf("wrapped: %w", timeout)), want: guardian.AddressFine},
		"timeout then missing":                  {err: errors.Join(fmt.Errorf("wrapped: %w", timeout), missing), want: guardian.AddressFine},
		"blocked then deadline":                 {err: errors.Join(blocked, context.DeadlineExceeded), want: guardian.AddressFine},
		"As timeout alone":                      {err: asNetError{timeout: true}, want: guardian.AddressFine},
		"blocked then As timeout":               {err: errors.Join(blocked, asNetError{timeout: true}), want: guardian.AddressFine},
		"As timeout then missing":               {err: errors.Join(asNetError{timeout: true}, missing), want: guardian.AddressFine},
		"As non-timeout, blocked, then timeout": {err: errors.Join(asNetError{timeout: false}, blocked, fmt.Errorf("wrapped: %w", timeout)), want: guardian.AddressFine},
		"As non-timeout then blocked":           {err: errors.Join(asNetError{timeout: false}, blocked), want: guardian.AddressNotAllowed},
		"As non-timeout then missing":           {err: errors.Join(asNetError{timeout: false}, missing), want: guardian.AddressNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, guardian.AddressFailureOf(tc.err, false))
			require.Equal(t, guardian.AddressFine, guardian.AddressFailureOf(tc.err, true), "a tunnel dial is never an address failure")
		})
	}
}
