package plugins

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
)

func TestAssignmentAdmissionAllowsWildcardNarrowingWhenUnavailable(t *testing.T) {
	t.Parallel()

	service := &Service{}
	guard := service.assignmentAdmissionGuard(admission.RolloutConfig{}, admission.ErrUnavailable)
	require.NoError(t, guard(t.Context(), nil, pluginsrepo.Plugin{}, []string{"*"}, []string{"user:member"}))
	require.NoError(t, guard(t.Context(), nil, pluginsrepo.Plugin{}, []string{"*"}, nil))
	require.NoError(t, guard(t.Context(), nil, pluginsrepo.Plugin{}, []string{"*"}, []string{"*"}))
}

func TestDistributionAdmissionErrorMappingsPreserveCauses(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		cause error
		code  oops.Code
	}{
		{cause: admission.ErrApprovalRequired, code: oops.CodeConflict},
		{cause: admission.ErrDistributionDisabled, code: oops.CodeConflict},
		{cause: admission.ErrUnavailable, code: oops.CodeUnavailable},
	} {
		err := mapDistributionAdmissionError(fmt.Errorf("guard: %w", test.cause))
		require.ErrorIs(t, err, test.cause)
		var shared *oops.ShareableError
		require.ErrorAs(t, err, &shared)
		require.Equal(t, test.code, shared.Code)
	}
}
