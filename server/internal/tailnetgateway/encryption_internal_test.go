package tailnetgateway

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/k8s"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

func TestEncryptionKeyUnavailableCoversCustomerKeyProblems(t *testing.T) {
	t.Parallel()

	for _, err := range []error{orgencryption.ErrSuspended, orgencryption.ErrKeyUnavailable, fmt.Errorf("%w: key was deleted", orgencryption.ErrInvalidKey)} {
		require.True(t, encryptionKeyUnavailable(err), "%v", err)
	}
	require.False(t, encryptionKeyUnavailable(errors.New("connection refused")))
	require.Equal(t, "organization encryption key unavailable", secretFailure(fmt.Errorf("decrypt: %w", orgencryption.ErrInvalidKey), "auth key unreadable"))
	require.Equal(t, "auth key unreadable", secretFailure(errors.New("corrupt ciphertext"), "auth key unreadable"))
}

func TestOperationErrorCode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{name: "missing ingress", err: fmt.Errorf("%w: ingress does not exist", k8s.ErrNetworkIngressInvalidDesiredState), want: k8s.NetworkIngressErrorInvalidDesiredState},
		{name: "database error", err: errors.New("load ingress owner: connection refused"), want: ""},
		{name: "rejected credentials", err: fmt.Errorf("%w: delete device", k8s.ErrNetworkIngressProviderCredentialsRejected), want: k8s.NetworkIngressErrorProviderCredentialsRejected},
		{name: "rate limited", err: fmt.Errorf("%w: delete service", k8s.ErrNetworkIngressProviderRateLimited), want: k8s.NetworkIngressErrorProviderRateLimited},
		{name: "key unusable", err: fmt.Errorf("decrypt credentials: %w", orgencryption.ErrSuspended), want: k8s.NetworkIngressErrorEncryptionKeyUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, operationErrorCode(tc.err))
		})
	}
}
