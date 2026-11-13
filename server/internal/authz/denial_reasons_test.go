package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

func TestDenialReasons_SkipsValidationWhenRBACIsNotEnforced(t *testing.T) {
	t.Parallel()

	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{ActiveOrganizationID: "org_123", UserID: sessionUserID, AccountType: "enterprise"})
	enforce, err := engine.ShouldEnforce(ctx)
	require.NoError(t, err)
	require.False(t, enforce, "a sessionless context skips RBAC")

	valid := MCPCheck(ScopeMCPConnect, "toolset_1", authProjectP)
	invalid := Check{Scope: ScopeMCPConnect, ResourceKind: "", ResourceID: "", Dimensions: nil}
	authClient := MCPAuthClientCheck("ac_1", MCPAuthClientDimensions{ClientType: AuthClientTypeServiceAccount, ProjectID: authProjectP, ConnectionOwner: "", UpstreamClient: ""})

	reasons, err := engine.DenialReasons(ctx, []Check{valid, invalid, authClient})
	require.NoError(t, err, "unenforced callers get the unenforced result, like FindMatched")
	require.Equal(t, []DenialReason{DenialReasonNone, DenialReasonNone, DenialReasonNotGranted}, reasons)
}
