package remotesessions

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFederatedRefreshSetupPreservesSignerOwnership(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		cause  error
		kind   FederatedRefreshFailure
		signer bool
	}{
		{"missing signer", errors.Join(ErrFederatedUnavailable, ErrFederatedSigning), FederatedRefreshSetupRetryable, true},
		{"live signer fault", ErrFederatedSigning, FederatedRefreshSetup, true},
		{"provider discovery outage", ErrFederatedUnavailable, FederatedRefreshSetupRetryable, false},
		{"configuration", ErrFederatedConfiguration, FederatedRefreshSetup, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := federatedRefreshSetupError(fmt.Errorf("private signer detail: %w", tc.cause))
			var failure *FederatedRefreshError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, tc.kind, failure.Kind)
			require.Equal(t, tc.signer, errors.Is(err, ErrFederatedSigning))
			require.NotContains(t, err.Error(), "private signer detail")
		})
	}
}
