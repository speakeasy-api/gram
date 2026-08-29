package proxy_test

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
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
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

		require.True(t, proxy.IsDeadPeerDialError(err), "cause %T %v", cause, cause)
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
		require.False(t, proxy.IsDeadPeerDialError(err), "error %T %v", err, err)
	}
}
