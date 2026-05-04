package authz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// newAPIKeyAuthCtx constructs a *contextvalues.AuthContext that mimics an
// API-key-authenticated request for the api_key principal RBAC tests below.
func newAPIKeyAuthCtx(orgID, apiKeyID, accountType string, systemManaged bool) *contextvalues.AuthContext {
	return &contextvalues.AuthContext{
		ActiveOrganizationID:  orgID,
		UserID:                "user_irrelevant_for_apikey",
		ExternalUserID:        "",
		APIKeyID:              apiKeyID,
		APIKeySystemManaged:   systemManaged,
		SessionID:             nil,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           accountType,
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          []string{"consumer"},
		IsAdmin:               false,
	}
}

// TestAPIKeyPrincipal_MatchingGrantSucceeds: a system-managed key with an
// mcp:connect grant succeeds against its bound toolset.
func TestAPIKeyPrincipal_MatchingGrantSucceeds(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	orgID := "org_apikey_match"
	apiKeyID := "00000000-0000-0000-0000-0000000000a1"
	toolsetID := "00000000-0000-0000-0000-0000000000b1"

	seedOrganization(t, t.Context(), conn, orgID)
	seedGrant(t, t.Context(), conn, orgID,
		urn.NewPrincipal(urn.PrincipalTypeAPIKey, apiKeyID),
		ScopeMCPConnect, toolsetID,
	)

	engine := NewEngine(testenv.NewLogger(t), conn, rbacAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	ctx := contextvalues.SetAuthContext(t.Context(), newAPIKeyAuthCtx(orgID, apiKeyID, "enterprise", true))
	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	err = engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: toolsetID})
	require.NoError(t, err, "scoped api-key must succeed against its bound toolset")
}

// TestAPIKeyPrincipal_MismatchedToolsetIsForbidden: a system-managed key
// with an mcp:connect grant is rejected against any other toolset.
func TestAPIKeyPrincipal_MismatchedToolsetIsForbidden(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	orgID := "org_apikey_mismatch"
	apiKeyID := "00000000-0000-0000-0000-0000000000a2"
	toolsetID := "00000000-0000-0000-0000-0000000000b2"
	otherToolsetID := "00000000-0000-0000-0000-0000000000c2"

	seedOrganization(t, t.Context(), conn, orgID)
	seedGrant(t, t.Context(), conn, orgID,
		urn.NewPrincipal(urn.PrincipalTypeAPIKey, apiKeyID),
		ScopeMCPConnect, toolsetID,
	)

	engine := NewEngine(testenv.NewLogger(t), conn, rbacAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	ctx := contextvalues.SetAuthContext(t.Context(), newAPIKeyAuthCtx(orgID, apiKeyID, "enterprise", true))
	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	err = engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: otherToolsetID})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// TestAPIKeyPrincipal_UserManagedBypasses: a user-managed key (system_managed=false)
// has no grants on its principal, so the existing CLI / producer / chat / hooks
// bypass policy still applies.
func TestAPIKeyPrincipal_UserManagedBypasses(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	orgID := "org_apikey_usermanaged"
	apiKeyID := "00000000-0000-0000-0000-0000000000a3"
	otherToolsetID := "00000000-0000-0000-0000-0000000000c3"

	seedOrganization(t, t.Context(), conn, orgID)

	engine := NewEngine(testenv.NewLogger(t), conn, rbacAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	ctx := contextvalues.SetAuthContext(t.Context(), newAPIKeyAuthCtx(orgID, apiKeyID, "enterprise", false))
	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	err = engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: otherToolsetID})
	require.NoError(t, err)
}

// TestAPIKeyPrincipal_NonEnterpriseStillEnforces: plugin scoping is a
// security primitive, not a tier feature — the AccountType / RBAC feature
// flag gates that govern session-authenticated requests don't apply to
// api-key-principal grants.
func TestAPIKeyPrincipal_NonEnterpriseStillEnforces(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	orgID := "org_apikey_nonenterprise"
	apiKeyID := "00000000-0000-0000-0000-0000000000a4"
	toolsetID := "00000000-0000-0000-0000-0000000000b4"
	otherToolsetID := "00000000-0000-0000-0000-0000000000c4"

	seedOrganization(t, t.Context(), conn, orgID)
	seedGrant(t, t.Context(), conn, orgID,
		urn.NewPrincipal(urn.PrincipalTypeAPIKey, apiKeyID),
		ScopeMCPConnect, toolsetID,
	)

	engine := NewEngine(testenv.NewLogger(t), conn, rbacAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	ctx := contextvalues.SetAuthContext(t.Context(), newAPIKeyAuthCtx(orgID, apiKeyID, "free", true))
	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	err = engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: otherToolsetID})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr,
		"plugin-scoped key must enforce regardless of account type")
	require.Equal(t, oops.CodeForbidden, oopsErr.Code)
}

// TestAPIKeyPrincipal_SystemManagedNoGrantsBypasses: a system-managed key
// that has no principal_grants rows (e.g. the org-wide hooks key) falls
// under the same bypass policy as user-managed keys.
func TestAPIKeyPrincipal_SystemManagedNoGrantsBypasses(t *testing.T) {
	t.Parallel()
	conn := newTestDB(t)
	orgID := "org_apikey_nogrants"
	apiKeyID := "00000000-0000-0000-0000-0000000000a5"
	otherToolsetID := "00000000-0000-0000-0000-0000000000c5"

	seedOrganization(t, t.Context(), conn, orgID)

	engine := NewEngine(testenv.NewLogger(t), conn, rbacAlwaysEnabled, workos.NewStubClient(), cache.NoopCache)
	ctx := contextvalues.SetAuthContext(t.Context(), newAPIKeyAuthCtx(orgID, apiKeyID, "enterprise", true))
	ctx, err := engine.PrepareContext(ctx)
	require.NoError(t, err)

	err = engine.Require(ctx, Check{Scope: ScopeMCPConnect, ResourceID: otherToolsetID})
	require.NoError(t, err, "system-managed key with zero grants must fall under the bypass policy")
}
