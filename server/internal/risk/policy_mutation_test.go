package risk

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestPolicyMutationErrorMapsCoreFailures(t *testing.T) {
	t.Parallel()

	service := &Service{logger: testenv.NewLogger(t)}
	cause := errors.New("database unavailable")

	tests := []struct {
		name        string
		err         error
		code        oops.Code
		message     string
		hiddenCause error
	}{
		{
			name:    "stale update",
			err:     &policycore.StalePolicyError{},
			code:    oops.CodeConflict,
			message: "risk policy changed during update; reload and retry",
		},
		{
			name:    "blocking policy conflict",
			err:     &policycore.BlockingPolicyConflictError{PolicyName: "existing blocker"},
			code:    oops.CodeConflict,
			message: `project already has an enabled shadow mcp blocking policy "existing blocker"; disable or delete it first`,
		},
		{
			name:        "mutation step failure",
			err:         &policycore.MutationError{Message: "lock risk policy", Cause: cause},
			code:        oops.CodeUnexpected,
			message:     "lock risk policy",
			hiddenCause: cause,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mapped := service.policyMutationError(t.Context(), tt.err)
			var shareable *oops.ShareableError
			require.ErrorAs(t, mapped, &shareable)
			require.Equal(t, tt.code, shareable.Code)
			require.Equal(t, tt.message, shareable.Error())
			if tt.hiddenCause != nil {
				require.ErrorIs(t, mapped, tt.hiddenCause)
			}
		})
	}
}
