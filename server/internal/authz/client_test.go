package authz

import (
	"testing"

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
	return NewEngineClient(NewEngine(testenv.NewLogger(t), nil, staticChallengeLogging(false), workos.NewStubClient()))
}

func clientTarget(serverID string) Target {
	return MCPServerTarget(clientOrg, clientProject, serverID)
}

func useServer(t *testing.T, serverID string) []Request {
	t.Helper()
	requests, err := MCPServerUse(clientTarget(serverID), MCPServerPrivate)
	require.NoError(t, err)
	return requests
}

func projectGrant(scope Scope, serverID, projectID string) Grant {
	selector := NewSelector(scope, serverID)
	selector[SelectorKeyProjectID] = projectID
	return NewGrantWithSelector(scope, selector)
}

func TestPermissionDefinitions_configurationNeverImpliesUse(t *testing.T) {
	t.Parallel()

	require.NotContains(t, ScopeImplicationClosure(ScopeMCPWrite), ScopeMCPConnect)
	require.NotContains(t, ScopeImplicationClosure(ScopeMCPRead), ScopeMCPConnect)
	require.Contains(t, ScopeImplicationClosure(ScopeMCPWrite), ScopeMCPRead, "write still lets a caller view the settings it edits")

	allowed, err := GrantsAuthorize([]Grant{NewGrant(ScopeMCPWrite, clientServer)}, MCPCheck(ScopeMCPConnect, clientServer, clientProject))
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestPermissionDefinitions_blockWinsOnlyForDefinedPermissions(t *testing.T) {
	t.Parallel()

	for _, scope := range []Scope{ScopeMCPRead, ScopeMCPWrite, ScopeMCPConnect} {
		require.False(t, ExclusionYieldsToDirectGrants(scope), scope)
	}
	// Families without a definition keep the direct-user override.
	for _, scope := range []Scope{ScopeMCPAuth, ScopeProjectRead, ScopeProjectWrite} {
		require.True(t, ExclusionYieldsToDirectGrants(scope), scope)
	}

	grants := []Grant{
		heldBy(clientRole, NewGrant(ScopeMCPBlockedConnect, clientServer)),
		heldBy(clientUser, NewGrant(ScopeMCPConnect, clientServer)),
	}
	allowed, err := GrantsAuthorize(grants, MCPCheck(ScopeMCPConnect, clientServer, clientProject))
	require.NoError(t, err)
	require.False(t, allowed, "a role block defeats a direct user allow")
}

func TestPermissionDefinitions_rejectsInconsistentDefinitions(t *testing.T) {
	t.Parallel()

	valid := AllDefinitions()
	require.NoError(t, validateDefinitions(valid))

	for name, mutate := range map[string]func([]PermissionDefinition){
		"duplicate":           func(d []PermissionDefinition) { d[1].Name = d[0].Name },
		"wrong resource":      func(d []PermissionDefinition) { d[0].Resource = ResourceKindProject },
		"no concrete anchor":  func(d []PermissionDefinition) { d[2].AllowedAnchors = []string{ResourceKindProject} },
		"implied by block":    func(d []PermissionDefinition) { d[0].ImpliedBy = []Scope{ScopeMCPBlockedWrite} },
		"implied across kind": func(d []PermissionDefinition) { d[0].ImpliedBy = []Scope{ScopeProjectWrite} },
		"missing block":       func(d []PermissionDefinition) { d[2].ExcludedBy = "" },
	} {
		defs := AllDefinitions()
		mutate(defs)
		require.Error(t, validateDefinitions(defs), name)
	}
}

func TestClient_refusesCrossTenantTargetDespiteGrant(t *testing.T) {
	t.Parallel()
	client := newClientTestClient(t)
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeMCPConnect, clientServer)})

	foreign := MCPServerTarget("org_other", clientProject, clientServer)
	decision, err := client.Authorize(ctx, Request{Permission: ScopeMCPConnect, Target: foreign})
	require.NoError(t, err)
	require.Equal(t, OutcomeDenied, decision.Outcome)
	require.Equal(t, ReasonCrossTenant, decision.Reason)

	_, err = client.Require(ctx, Request{Permission: ScopeMCPConnect, Target: foreign})
	requireForbidden(t, err)

	// A project ancestor from another organization is refused too.
	mixed := clientTarget(clientServer)
	mixed.Ancestors[0].OrganizationID = "org_other"
	decision, err = client.Authorize(ctx, Request{Permission: ScopeMCPConnect, Target: mixed})
	require.NoError(t, err)
	require.Equal(t, ReasonCrossTenant, decision.Reason)
}

func TestClient_missingAncestorIsUnresolved(t *testing.T) {
	t.Parallel()
	client := newClientTestClient(t)
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeMCPConnect, clientServer)})

	target := clientTarget(clientServer)
	target.Ancestors = target.Ancestors[1:]
	decision, err := client.Authorize(ctx, Request{Permission: ScopeMCPConnect, Target: target})
	require.NoError(t, err)
	require.Equal(t, OutcomeUnresolved, decision.Outcome)
	require.Equal(t, ReasonMissingFact, decision.Reason)
	require.False(t, decision.Allowed())

	_, err = client.Require(ctx, Request{Permission: ScopeMCPConnect, Target: target})
	requireForbidden(t, err)
}

func TestClient_rejectsMalformedRequests(t *testing.T) {
	t.Parallel()
	client := newClientTestClient(t)
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeRoot, WildcardResource)})

	for name, request := range map[string]Request{
		"undefined permission": {Permission: ScopeProjectRead, Target: clientTarget(clientServer)},
		"wildcard target":      {Permission: ScopeMCPConnect, Target: clientTarget(WildcardResource)},
		"wrong kind":           {Permission: ScopeMCPConnect, Target: Target{Resource: ResourceRef{OrganizationID: clientOrg, Kind: ResourceKindProject, ID: clientProject}, Ancestors: nil}},
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
		grant Grant
		basis DecisionBasis
	}{
		"wildcard connect":         {NewGrant(ScopeMCPConnect, WildcardResource), BasisLegacyBroadGrant},
		"project-wide connect":     {projectGrant(ScopeMCPConnect, WildcardResource, clientProject), BasisLegacyBroadGrant},
		"one-server connect":       {NewGrant(ScopeMCPConnect, clientServer), BasisNone},
		"project-bound one-server": {projectGrant(ScopeMCPConnect, clientServer, clientProject), BasisNone},
	} {
		ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{tc.grant})
		decision, err := client.Authorize(ctx, useServer(t, clientServer)...)
		require.NoError(t, err, name)
		require.True(t, decision.Allowed(), name)
		require.Equal(t, tc.basis, decision.Basis, name)

		required, err := client.Require(ctx, useServer(t, clientServer)...)
		require.NoError(t, err, name)
		require.Equal(t, decision, required, name)
	}

	// A wildcard write grant is an allowed anchor for configuration.
	ctx := GrantsToContext(enterpriseSessionCtx(t), []Grant{NewGrant(ScopeMCPWrite, WildcardResource)})
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

	operator := []Grant{heldBy(clientUser, NewGrant(ScopeMCPWrite, clientServer))}
	connector := []Grant{heldBy(clientUser, NewGrant(ScopeMCPConnect, clientServer))}
	blocked := []Grant{
		heldBy(clientUser, NewGrant(ScopeMCPConnect, clientServer)),
		heldBy(clientUser, NewGrant(ScopeMCPWrite, clientServer)),
		heldBy(clientRole, NewGrant(ScopeMCPBlockedConnect, clientServer)),
		heldBy(clientRole, NewGrant(ScopeMCPBlockedWrite, clientServer)),
	}
	projScoped := []Grant{
		heldBy(clientUser, projectGrant(ScopeMCPConnect, clientServer, clientProject)),
		heldBy(clientUser, projectGrant(ScopeMCPConnect, clientOtherSrv, "project-2")),
	}
	pin := func(serverID string) []Request { return MCPScopePin(clientTarget(serverID), nil) }

	for _, tc := range []struct {
		name     string
		grants   []Grant
		requests []Request
		reason   DecisionReason
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
		ctx := GrantsToContext(enterpriseSessionCtx(t), tc.grants)
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
	require.Equal(t, []Request{{Permission: ScopeMCPConnect, Target: clientTarget(clientServer)}}, requests)

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
