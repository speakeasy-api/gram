package platformmcp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	platformhttp "github.com/speakeasy-api/gram/server/gen/http/platform_mcp/server"
	pluginshttp "github.com/speakeasy-api/gram/server/gen/http/plugins/server"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestLifecycleToolRetainsDistributionErrorCodes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		cause error
		code  string
	}{
		{cause: ErrDistributionBlockedPendingApproval, code: "approval_required"},
		{cause: ErrDistributionDisabled, code: "distribution_disabled"},
		{cause: ErrDistributionAdmissionUnavailable, code: "distribution_unavailable"},
	} {
		result, ok := operationBudgetToolResult(fmt.Errorf("enable admission: %w", test.cause))
		require.True(t, ok)
		require.True(t, result.IsError)
		require.Len(t, result.Content, 1)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		require.Contains(t, text.Text, `"code":"`+test.code+`"`)
	}
}

func TestManagementAdmissionUnavailableEncodesHTTP503(t *testing.T) {
	t.Parallel()
	service := &ManagementService{}
	cause := fmt.Errorf("admission: %w", ErrDistributionAdmissionUnavailable)
	err := service.mapOnboardingError(cause)
	require.ErrorIs(t, err, cause)
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	recorder := httptest.NewRecorder()
	encode := platformhttp.EncodeDistributeOnboardingCandidateError(goahttp.ResponseEncoder, nil)
	require.NoError(t, encode(t.Context(), recorder, shareable.AsGoa(t.Context())))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestPluginAssignmentUnavailableEncoderRetainsConflictMapping(t *testing.T) {
	t.Parallel()
	encode := pluginshttp.EncodeSetPluginAssignmentsError(goahttp.ResponseEncoder, nil)
	for _, code := range []oops.Code{oops.CodeUnavailable, oops.CodeConflict} {
		err := oops.E(code, nil, "distribution admission refused")
		recorder := httptest.NewRecorder()
		require.NoError(t, encode(t.Context(), recorder, err.AsGoa(t.Context())))
		require.Equal(t, err.HTTPStatus(t.Context()), recorder.Code)
	}
}
