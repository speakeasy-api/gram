package catalog

import (
	"context"
	"slices"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/stretchr/testify/require"
)

const (
	clientOrg      = "org_123"
	clientUser     = "user:user_123"
	clientRole     = "role:organization:role-1"
	clientProject  = "project-1"
	clientServer   = "server-1"
	clientOtherSrv = "server-2"
)

func newClientTestClient(t *testing.T) *Client {
	t.Helper()
	noChallengeLogging := func(context.Context, string) (bool, error) { return false, nil }
	return NewEngineClient(authz.NewEngine(testenv.NewLogger(t), nil, noChallengeLogging, workos.NewStubClient()))
}

func sessionCtx(t *testing.T) context.Context {
	t.Helper()
	sessionID := "session_123"
	return contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  clientOrg,
		UserID:                "user_123",
		ExternalUserID:        "",
		APIKeyID:              "",
		SessionID:             &sessionID,
		ProjectID:             nil,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "enterprise",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
	})
}

func heldBy(principal string, grant authz.Grant) authz.Grant {
	grant.PrincipalUrn = principal
	return grant
}

func requireForbidden(t *testing.T, err error) {
	t.Helper()
	var shareable *oops.ShareableError
	require.ErrorAs(t, err, &shareable)
	require.Equal(t, oops.CodeForbidden, shareable.Code)
}

func clientTarget(serverID string) Target {
	return MCPServerTarget(clientOrg, clientProject, serverID)
}

// TestDefinitionsMatchEngine keeps the engine's own scope tables, which authz
// cannot read from this package, in line with the registry.
func TestDefinitionsMatchEngine(t *testing.T) {
	t.Parallel()

	for _, def := range Definitions() {
		for _, other := range Definitions() {
			if other.Name == def.Name {
				continue
			}
			require.Equal(t, slices.Contains(def.ImpliedBy, other.Name), slices.Contains(authz.ScopeImplicationClosure(other.Name), def.Name), "%s implied by %s", def.Name, other.Name)
		}
		exclusion, ok := authz.ExclusionScopeFor(def.Name)
		require.True(t, ok, def.Name)
		require.Equal(t, def.ExcludedBy, exclusion, def.Name)
		require.Equal(t, !def.BlockWins, authz.ExclusionYieldsToDirectGrants(def.Name), def.Name)
	}
}

func useServer(t *testing.T, serverID string) []Request {
	t.Helper()
	requests, err := MCPServerUse(clientTarget(serverID), MCPServerPrivate)
	require.NoError(t, err)
	return requests
}

func projectGrant(scope authz.Scope, serverID, projectID string) authz.Grant {
	selector := authz.NewSelector(scope, serverID)
	selector[authz.SelectorKeyProjectID] = projectID
	return authz.NewGrantWithSelector(scope, selector)
}

func TestDefinitions_configurationNeverImpliesUse(t *testing.T) {
	t.Parallel()

	require.NotContains(t, authz.ScopeImplicationClosure(authz.ScopeMCPWrite), authz.ScopeMCPConnect)
	require.NotContains(t, authz.ScopeImplicationClosure(authz.ScopeMCPRead), authz.ScopeMCPConnect)
	require.Contains(t, authz.ScopeImplicationClosure(authz.ScopeMCPWrite), authz.ScopeMCPRead, "write still lets a caller view the settings it edits")

	allowed, err := authz.GrantsAuthorize([]authz.Grant{authz.NewGrant(authz.ScopeMCPWrite, clientServer)}, authz.MCPCheck(authz.ScopeMCPConnect, clientServer, clientProject))
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestDefinitions_blockWinsOnlyForDefinedPermissions(t *testing.T) {
	t.Parallel()

	for _, scope := range []authz.Scope{authz.ScopeMCPRead, authz.ScopeMCPWrite, authz.ScopeMCPConnect} {
		require.False(t, authz.ExclusionYieldsToDirectGrants(scope), scope)
	}
	// Families without a definition keep the direct-user override.
	for _, scope := range []authz.Scope{authz.ScopeMCPAuth, authz.ScopeProjectRead, authz.ScopeProjectWrite} {
		require.True(t, authz.ExclusionYieldsToDirectGrants(scope), scope)
	}

	grants := []authz.Grant{
		heldBy(clientRole, authz.NewGrant(authz.ScopeMCPBlockedConnect, clientServer)),
		heldBy(clientUser, authz.NewGrant(authz.ScopeMCPConnect, clientServer)),
	}
	allowed, err := authz.GrantsAuthorize(grants, authz.MCPCheck(authz.ScopeMCPConnect, clientServer, clientProject))
	require.NoError(t, err)
	require.False(t, allowed, "a role block defeats a direct user allow")
}

func TestDefinitions_rejectsInconsistentDefinitions(t *testing.T) {
	t.Parallel()

	valid := Definitions()
	require.NoError(t, validate(valid))

	for name, mutate := range map[string]func([]Definition){
		"duplicate":           func(d []Definition) { d[1].Name = d[0].Name },
		"wrong resource":      func(d []Definition) { d[0].Resource = authz.ResourceKindProject },
		"no concrete anchor":  func(d []Definition) { d[2].AllowedAnchors = []string{authz.ResourceKindProject} },
		"implied by block":    func(d []Definition) { d[0].ImpliedBy = []authz.Scope{authz.ScopeMCPBlockedWrite} },
		"implied across kind": func(d []Definition) { d[0].ImpliedBy = []authz.Scope{authz.ScopeProjectWrite} },
		"missing block":       func(d []Definition) { d[2].ExcludedBy = "" },
	} {
		defs := Definitions()
		mutate(defs)
		require.Error(t, validate(defs), name)
	}
}

func TestClient_refusesCrossTenantTargetDespiteGrant(t *testing.T) {
	t.Parallel()
	client := newClientTestClient(t)
	ctx := authz.GrantsToContext(sessionCtx(t), []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, clientServer)})

	foreign := MCPServerTarget("org_other", clientProject, clientServer)
	decision, err := client.Authorize(ctx, Request{Permission: authz.ScopeMCPConnect, Target: foreign})
	require.NoError(t, err)
	require.Equal(t, OutcomeDenied, decision.Outcome)
	require.Equal(t, ReasonCrossTenant, decision.Reason)

	_, err = client.Require(ctx, Request{Permission: authz.ScopeMCPConnect, Target: foreign})
	requireForbidden(t, err)

	// A project ancestor from another organization is refused too.
	mixed := clientTarget(clientServer)
	mixed.Ancestors[0].OrganizationID = "org_other"
	decision, err = client.Authorize(ctx, Request{Permission: authz.ScopeMCPConnect, Target: mixed})
	require.NoError(t, err)
	require.Equal(t, ReasonCrossTenant, decision.Reason)
}

func TestClient_missingAncestorIsUnresolved(t *testing.T) {
	t.Parallel()
	client := newClientTestClient(t)
	ctx := authz.GrantsToContext(sessionCtx(t), []authz.Grant{authz.NewGrant(authz.ScopeMCPConnect, clientServer)})

	target := clientTarget(clientServer)
	target.Ancestors = target.Ancestors[1:]
	decision, err := client.Authorize(ctx, Request{Permission: authz.ScopeMCPConnect, Target: target})
	require.NoError(t, err)
	require.Equal(t, OutcomeUnresolved, decision.Outcome)
	require.Equal(t, ReasonMissingFact, decision.Reason)
	require.False(t, decision.Allowed())

	_, err = client.Require(ctx, Request{Permission: authz.ScopeMCPConnect, Target: target})
	requireForbidden(t, err)
}

func TestClient_rejectsMalformedRequests(t *testing.T) {
	t.Parallel()
	client := newClientTestClient(t)
	ctx := authz.GrantsToContext(sessionCtx(t), []authz.Grant{authz.NewGrant(authz.ScopeRoot, authz.WildcardResource)})

	for name, request := range map[string]Request{
		"undefined permission": {Permission: authz.ScopeProjectRead, Target: clientTarget(clientServer)},
		"wildcard target":      {Permission: authz.ScopeMCPConnect, Target: clientTarget(authz.WildcardResource)},
		"wrong kind":           {Permission: authz.ScopeMCPConnect, Target: Target{Resource: ResourceRef{OrganizationID: clientOrg, Kind: authz.ResourceKindProject, ID: clientProject}, Ancestors: nil}},
	} {
		_, err := client.Authorize(ctx, request)
		var shareable *oops.ShareableError
		require.ErrorAs(t, err, &shareable, name)
		require.Equal(t, oops.CodeUnexpected, shareable.Code, name)
	}
}

func TestClient_labelsLegacyBroadConnectGrant(t *testing.T) {
	t.Parallel()
	client := newClientTestClient(t)

	for name, tc := range map[string]struct {
		grant authz.Grant
		basis Basis
	}{
		"wildcard connect":         {authz.NewGrant(authz.ScopeMCPConnect, authz.WildcardResource), BasisLegacyBroadGrant},
		"project-wide connect":     {projectGrant(authz.ScopeMCPConnect, authz.WildcardResource, clientProject), BasisLegacyBroadGrant},
		"one-server connect":       {authz.NewGrant(authz.ScopeMCPConnect, clientServer), BasisNone},
		"project-bound one-server": {projectGrant(authz.ScopeMCPConnect, clientServer, clientProject), BasisNone},
	} {
		ctx := authz.GrantsToContext(sessionCtx(t), []authz.Grant{tc.grant})
		decision, err := client.Authorize(ctx, useServer(t, clientServer)...)
		require.NoError(t, err, name)
		require.True(t, decision.Allowed(), name)
		require.Equal(t, tc.basis, decision.Basis, name)

		required, err := client.Require(ctx, useServer(t, clientServer)...)
		require.NoError(t, err, name)
		require.Equal(t, decision, required, name)
	}

	// A wildcard write grant is an allowed anchor for configuration.
	ctx := authz.GrantsToContext(sessionCtx(t), []authz.Grant{authz.NewGrant(authz.ScopeMCPWrite, authz.WildcardResource)})
	decision, err := client.Authorize(ctx, MCPScopePin(clientTarget(clientServer), nil)...)
	require.NoError(t, err)
	require.True(t, decision.Allowed())
	require.Equal(t, BasisNone, decision.Basis)
}

// TestClient_authorizeAgreesWithRequire runs the slice 1 grant rows through
// both entry points: the capability answer must equal the enforced one.
func TestClient_authorizeAgreesWithRequire(t *testing.T) {
	t.Parallel()
	client := newClientTestClient(t)

	operator := []authz.Grant{heldBy(clientUser, authz.NewGrant(authz.ScopeMCPWrite, clientServer))}
	connector := []authz.Grant{heldBy(clientUser, authz.NewGrant(authz.ScopeMCPConnect, clientServer))}
	blocked := []authz.Grant{
		heldBy(clientUser, authz.NewGrant(authz.ScopeMCPConnect, clientServer)),
		heldBy(clientUser, authz.NewGrant(authz.ScopeMCPWrite, clientServer)),
		heldBy(clientRole, authz.NewGrant(authz.ScopeMCPBlockedConnect, clientServer)),
		heldBy(clientRole, authz.NewGrant(authz.ScopeMCPBlockedWrite, clientServer)),
	}
	projScoped := []authz.Grant{
		heldBy(clientUser, projectGrant(authz.ScopeMCPConnect, clientServer, clientProject)),
		heldBy(clientUser, projectGrant(authz.ScopeMCPConnect, clientOtherSrv, "project-2")),
	}
	pin := func(serverID string) []Request { return MCPScopePin(clientTarget(serverID), nil) }

	for _, tc := range []struct {
		name     string
		grants   []authz.Grant
		requests []Request
		reason   Reason
	}{
		{"operator pins", operator, pin(clientServer), ReasonAllowed},
		{"operator cannot use", operator, useServer(t, clientServer), ReasonNoGrant},
		{"connector uses its server", connector, useServer(t, clientServer), ReasonAllowed},
		{"connector cannot use another", connector, useServer(t, clientOtherSrv), ReasonNoGrant},
		{"connector cannot pin", connector, pin(clientServer), ReasonNoGrant},
		{"block defeats direct connect", blocked, useServer(t, clientServer), ReasonBlocked},
		{"block defeats direct write", blocked, pin(clientServer), ReasonBlocked},
		{"project grant in its project", projScoped, useServer(t, clientServer), ReasonAllowed},
		{"project grant outside its project", projScoped, useServer(t, clientOtherSrv), ReasonNoGrant},
		{"pin needs every sharing server", operator, MCPScopePin(clientTarget(clientServer), []Target{clientTarget(clientOtherSrv)}), ReasonNoGrant},
	} {
		ctx := authz.GrantsToContext(sessionCtx(t), tc.grants)
		decision, err := client.Authorize(ctx, tc.requests...)
		require.NoError(t, err, tc.name)
		require.Equal(t, tc.reason, decision.Reason, tc.name)

		_, requireErr := client.Require(ctx, tc.requests...)
		if decision.Allowed() {
			require.NoError(t, requireErr, tc.name)
		} else {
			requireForbidden(t, requireErr)
		}
	}
}

func TestMCPServerUse(t *testing.T) {
	t.Parallel()

	requests, err := MCPServerUse(clientTarget(clientServer), MCPServerPublic)
	require.NoError(t, err)
	require.Empty(t, requests)

	requests, err = MCPServerUse(clientTarget(clientServer), MCPServerPrivate)
	require.NoError(t, err)
	require.Equal(t, []Request{{Permission: authz.ScopeMCPConnect, Target: clientTarget(clientServer)}}, requests)

	_, err = MCPServerUse(clientTarget(clientServer), "disabled")
	require.Error(t, err)
}

func TestMCPScopePin_dedupesSharingServers(t *testing.T) {
	t.Parallel()

	requests := MCPScopePin(clientTarget(clientServer), []Target{clientTarget(clientServer), clientTarget(clientOtherSrv), clientTarget(clientOtherSrv)})
	require.Len(t, requests, 2)
	require.Equal(t, clientServer, requests[0].Target.Resource.ID)
	require.Equal(t, clientOtherSrv, requests[1].Target.Resource.ID)
}
