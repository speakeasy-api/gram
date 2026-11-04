package remotesessions

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFederatedRefreshSetupClassifiesSignerFaults(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		cause error
		kind  FederatedRefreshFailure
	}{
		{"missing signer", errors.Join(ErrFederatedUnavailable, ErrFederatedSigning), FederatedRefreshSetupRetryable},
		{"live signer fault", ErrFederatedSigning, FederatedRefreshSetup},
		{"provider discovery outage", ErrFederatedUnavailable, FederatedRefreshSetupRetryable},
		{"configuration", ErrFederatedConfiguration, FederatedRefreshSetup},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := federatedRefreshSetupError(fmt.Errorf("private signer detail: %w", tc.cause))
			var failure *FederatedRefreshError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, tc.kind, failure.Kind)
			require.NotContains(t, err.Error(), "private signer detail")
		})
	}
}
