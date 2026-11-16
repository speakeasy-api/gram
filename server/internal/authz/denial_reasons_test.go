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

// A call refused only because the caller's grant names another disposition
// than read_only is reported as a disposition refusal, even though a missing
// key never matches on a decision.
func TestDenialReasons_OtherDispositionIsReported(t *testing.T) {
	t.Parallel()

	selector := NewSelector(ScopeMCPConnect, "toolset_1")
	selector[SelectorKeyDisposition] = DispositionIdempotent
	engine := NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient())
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrantWithSelector(ScopeMCPConnect, selector)})

	call := MCPToolCallCheck("toolset_1", MCPToolCallDimensions{Tool: "delete_all", Disposition: DispositionDestructive, ProjectID: authProjectP})
	reasons, err := engine.DenialReasons(ctx, []Check{call})
	require.NoError(t, err)
	require.Equal(t, []DenialReason{DenialReasonDisposition}, reasons)
}
