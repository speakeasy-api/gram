package mcpapproval_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// seedShadowMCPPolicy plants an enabled blocking shadow-MCP policy the way
// the policy setup flow writes them, returning its id.
func seedShadowMCPPolicy(t *testing.T, ctx context.Context, ti *testInstance, disposition string) uuid.UUID {
	t.Helper()

	return seedShadowMCPPolicyWith(t, ctx, ti, disposition, "block", true)
}

// seedShadowMCPPolicyWith plants a shadow-MCP policy with a chosen action and
// enabled state. An empty disposition stays NULL, the way legacy policies
// created before the disposition column exist in the wild.
func seedShadowMCPPolicyWith(t *testing.T, ctx context.Context, ti *testInstance, disposition, action string, enabled bool) uuid.UUID {
	t.Helper()

	policyID := uuid.New()
	_, err := riskrepo.New(ti.conn).CreateRiskPolicy(ctx, riskrepo.CreateRiskPolicyParams{
		ID:                   policyID,
		ProjectID:            ti.projectID,
		OrganizationID:       ti.organizationID,
		Name:                 "shadow mcp policy " + policyID.String()[:8],
		Sources:              []string{"shadow_mcp"},
		AnalyzerConfig:       []byte(`{}`),
		DisabledRules:        nil,
		ScopeExempt:          pgtype.Text{},
		Enabled:              enabled,
		Action:               action,
		AudienceType:         "everyone",
		AutoName:             false,
		UserMessage:          pgtype.Text{},
		ShadowMcpDisposition: conv.ToPGTextEmpty(disposition),
	})
	require.NoError(t, err)

	return policyID
}

// grantPrincipals reads the principal URNs granted for one server URL under
// the given scope on a policy.
func grantPrincipals(t *testing.T, ctx context.Context, ti *testInstance, scope authz.Scope, policyID uuid.UUID, canonicalURL string) []string {
	t.Helper()

	grants, err := authz.ListGrantsForResource(ctx, ti.conn, authz.Resource{
		OrganizationID: ti.organizationID,
		Scope:          scope,
		ResourceID:     policyID.String(),
	})
	require.NoError(t, err)

	principals := make([]string, 0, len(grants))
	for _, grant := range grants {
		if grant.Selector[authz.SelectorKeyServerURL] != canonicalURL {
			continue
		}
		principals = append(principals, grant.PrincipalUrn)
	}

	return principals
}

// An approval on a block_all policy is the enforcement change itself: the
// decision's principals become the server's bypass audience, in the same
// transaction that records the decision.
func TestRecordDecision_ApprovalMintsBypassGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicy(t, ctx, ti, "block_all")
	serverURL := "https://mcp.example.com/enforced"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})

	decision, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)

	// No principals named means everyone, and the decision row says so
	// explicitly rather than storing an empty set.
	allUsers := authz.AllUsersPrincipal().String()
	require.Equal(t, []string{allUsers}, decision.GrantedPrincipalUrns)
	require.Equal(t, []string{allUsers}, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, serverURL))

	// A later denial revokes the audience: the policy's blocked default
	// stands again, and the history keeps both decisions.
	_, err = ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "denied"))
	require.NoError(t, err)
	require.Empty(t, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, serverURL))
}

// Named principals become the exact bypass audience — the blast radius is the
// decision's, not the policy's.
func TestRecordDecision_ApprovalGrantsNamedPrincipals(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicy(t, ctx, ti, "block_all")
	serverURL := "https://mcp.example.com/scoped"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})

	principal := seedMemberPrincipal(t, ctx, ti, "user-blast-radius")
	payload := decisionPayload(requestID.String(), "approved")
	payload.GrantedPrincipalUrns = []string{principal}

	decision, err := ti.service.RecordDecision(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, []string{principal}, decision.GrantedPrincipalUrns)
	require.Equal(t, []string{principal}, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, serverURL))
}

// Under an allow_all policy the directions invert: a denial writes the block
// rule for everyone, and a later approval clears it.
func TestRecordDecision_AllowAllPolicyInverts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicy(t, ctx, ti, "allow_all")
	serverURL := "https://mcp.example.com/allow-all"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})

	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "denied"))
	require.NoError(t, err)
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBlock, policyID, serverURL))

	_, err = ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)
	require.Empty(t, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBlock, policyID, serverURL))
}

// An stdio target has no URL to key a grant on: the decision records without
// touching enforcement state.
func TestRecordDecision_StdioTargetRecordsWithoutGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicy(t, ctx, ti, "block_all")
	requestID := seedUnresolvedRequest(t, ctx, ti, ti.projectID, "npx -y some-local-server")

	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)

	grants, err := authz.ListGrantsForResource(ctx, ti.conn, authz.Resource{
		OrganizationID: ti.organizationID,
		Scope:          authz.ScopeRiskPolicyBypass,
		ResourceID:     policyID.String(),
	})
	require.NoError(t, err)
	require.Empty(t, grants)
}

// An approval narrower than everyone is rejected when only allow_all policies
// govern: the evaluator cannot express it, and silently widening would make
// the recorded blast radius a lie.
func TestRecordDecision_NarrowApprovalRejectedUnderAllowAllOnly(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	seedShadowMCPPolicy(t, ctx, ti, "allow_all")
	serverURL := "https://mcp.example.com/narrow"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})

	payload := decisionPayload(requestID.String(), "approved")
	payload.GrantedPrincipalUrns = []string{seedMemberPrincipal(t, ctx, ti, "someone")}

	_, err := ti.service.RecordDecision(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	// Nothing was written: the decision and the grant change fail together.
	require.Empty(t, decisionsFor(t, ctx, ti, ti.projectID, requestID))
	require.Equal(t, "requested", requestStatus(t, ctx, ti, ti.projectID, requestID))
}

// Deciding a promoted request resolves the legacy bypass request it grew out
// of, in the same transaction: the original ask must not stay pending in the
// legacy queue (and on the inventory's request counters) after its review is
// decided.
func TestRecordDecision_ResolvesPromotedBypassRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverURL := "https://mcp.example.com/promoted-drain"
	bypassID := seedBypassRequest(t, ctx, ti, ti.projectID, serverURL, "blocked-user", "hit the block")

	promoted, err := ti.service.Promote(ctx, promotePayload(bypassID.String()))
	require.NoError(t, err)

	_, err = ti.service.RecordDecision(ctx, decisionPayload(promoted.ID, "approved"))
	require.NoError(t, err)

	bypass, err := riskrepo.New(ti.conn).GetRiskPolicyBypassRequest(ctx, riskrepo.GetRiskPolicyBypassRequestParams{
		ID:        bypassID,
		ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	require.Equal(t, "approved", bypass.Status)
	require.True(t, bypass.DecidedBy.Valid)
}

// A principal that does not resolve in the organization is rejected before
// anything is written: a decision must never record an audience its grants
// cannot enforce.
func TestRecordDecision_UnresolvablePrincipalRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	seedShadowMCPPolicy(t, ctx, ti, "block_all")
	serverURL := "https://mcp.example.com/unresolvable"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})

	payload := decisionPayload(requestID.String(), "approved")
	payload.GrantedPrincipalUrns = []string{urn.NewPrincipal(urn.PrincipalTypeUser, "user-not-a-member").String()}

	_, err := ti.service.RecordDecision(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	require.Empty(t, decisionsFor(t, ctx, ti, ti.projectID, requestID))
	require.Equal(t, "requested", requestStatus(t, ctx, ti, ti.projectID, requestID))
}

// With a block_all policy alongside an allow_all one, a narrow approval
// composes correctly: the allow_all block rule clears, and the block_all
// bypass audience gates who actually passes.
func TestRecordDecision_NarrowApprovalComposesAcrossDispositions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	blockAllPolicy := seedShadowMCPPolicy(t, ctx, ti, "block_all")
	allowAllPolicy := seedShadowMCPPolicy(t, ctx, ti, "allow_all")
	serverURL := "https://mcp.example.com/composed"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})

	principal := seedMemberPrincipal(t, ctx, ti, "narrow-user")
	payload := decisionPayload(requestID.String(), "approved")
	payload.GrantedPrincipalUrns = []string{principal}

	_, err := ti.service.RecordDecision(ctx, payload)
	require.NoError(t, err)

	require.Equal(t, []string{principal}, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, blockAllPolicy, serverURL))
	require.Empty(t, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBlock, allowAllPolicy, serverURL))
}

// seedLegacyVariantBypassGrant plants a bypass grant the way legacy
// access-request approvals persisted them: a selector carrying both the
// server URL and a server identity. At runtime these evaluate URL-only, so a
// decision that only revoked the exact URL-only selector would leave them
// enforcing.
func seedLegacyVariantBypassGrant(t *testing.T, ctx context.Context, ti *testInstance, policyID uuid.UUID, serverURL string, principal urn.Principal) {
	t.Helper()

	selector := authz.NewSelector(authz.ScopeRiskPolicyBypass, policyID.String())
	selector[authz.SelectorKeyServerURL] = serverURL
	selector[authz.SelectorKeyServerIdentity] = "legacy-identity"

	require.NoError(t, authz.GrantResourceToPrincipals(ctx, ti.conn, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: ti.organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID.String(),
		},
		Principals: []urn.Principal{principal},
		Selector:   selector,
	}))
}

// A recorded denial must strip legacy {server_url, server_identity} variant
// grants too — those selectors evaluate URL-only at runtime, so leaving them
// standing would let a legacy-approved user survive the deny.
func TestRecordDecision_DenyRevokesLegacyVariantGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicy(t, ctx, ti, "block_all")
	serverURL := "https://mcp.example.com/legacy-variant"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})

	legacyPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "legacy-approved-user")
	seedLegacyVariantBypassGrant(t, ctx, ti, policyID, serverURL, legacyPrincipal)
	require.Equal(t, []string{legacyPrincipal.String()}, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, serverURL))

	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "denied"))
	require.NoError(t, err)

	require.Empty(t, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, serverURL))
}

// An approval replaces the whole audience, legacy variants included: the
// decision's principals are the audience afterwards, with no legacy grant
// silently widening it.
func TestRecordDecision_ApprovalReplacesLegacyVariantGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicy(t, ctx, ti, "block_all")
	serverURL := "https://mcp.example.com/legacy-replaced"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})

	legacyPrincipal := urn.NewPrincipal(urn.PrincipalTypeUser, "legacy-approved-user")
	seedLegacyVariantBypassGrant(t, ctx, ti, policyID, serverURL, legacyPrincipal)

	principal := seedMemberPrincipal(t, ctx, ti, "user-fresh-audience")
	payload := decisionPayload(requestID.String(), "approved")
	payload.GrantedPrincipalUrns = []string{principal}

	_, err := ti.service.RecordDecision(ctx, payload)
	require.NoError(t, err)

	require.Equal(t, []string{principal}, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, serverURL))
}

// A policy from before the disposition column carries NULL — which enforces
// exactly like block_all, the way the policy setup flow defaults it.
func TestRecordDecision_NullDispositionDefaultsToBlockAll(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicyWith(t, ctx, ti, "", "block", true)
	serverURL := "https://mcp.example.com/null-disposition"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})

	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)

	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, serverURL))
}

// A denial then an approval re-mints the bypass audience under block_all: the
// newest decision is what enforces, in either direction.
func TestRecordDecision_DenyThenApproveRemintsGrants(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	policyID := seedShadowMCPPolicy(t, ctx, ti, "block_all")
	serverURL := "https://mcp.example.com/re-minted"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})

	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "denied"))
	require.NoError(t, err)
	require.Empty(t, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, serverURL))

	_, err = ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)
	require.Equal(t, []string{authz.AllUsersPrincipal().String()}, grantPrincipals(t, ctx, ti, authz.ScopeRiskPolicyBypass, policyID, serverURL))

	require.Equal(t, "approved", requestStatus(t, ctx, ti, ti.projectID, requestID))
}

// Only enabled blocking policies enforce a decision: flag policies and
// disabled block policies are skipped entirely.
func TestRecordDecision_SkipsNonBlockingAndDisabledPolicies(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	flagPolicy := seedShadowMCPPolicyWith(t, ctx, ti, "block_all", "flag", true)
	disabledPolicy := seedShadowMCPPolicyWith(t, ctx, ti, "block_all", "block", false)
	serverURL := "https://mcp.example.com/skipped-policies"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "requested", evidence: "", version: 0})

	_, err := ti.service.RecordDecision(ctx, decisionPayload(requestID.String(), "approved"))
	require.NoError(t, err)

	for _, policyID := range []uuid.UUID{flagPolicy, disabledPolicy} {
		grants, err := authz.ListGrantsForResource(ctx, ti.conn, authz.Resource{
			OrganizationID: ti.organizationID,
			Scope:          authz.ScopeRiskPolicyBypass,
			ResourceID:     policyID.String(),
		})
		require.NoError(t, err)
		require.Empty(t, grants)
	}
}

// A role principal that names no role in the organization is the caller's
// error: rejected as a bad request before anything is written, not recorded
// as an audience the grants can never enforce.
func TestRecordDecision_UnknownRolePrincipalRejected(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	seedShadowMCPPolicy(t, ctx, ti, "block_all")
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://mcp.example.com/unknown-role", status: "requested", evidence: "", version: 0})

	payload := decisionPayload(requestID.String(), "approved")
	payload.GrantedPrincipalUrns = []string{urn.NewPrincipal(urn.PrincipalTypeRole, "organization:"+uuid.NewString()).String()}

	_, err := ti.service.RecordDecision(ctx, payload)
	requireOopsCode(t, err, oops.CodeBadRequest)

	require.Empty(t, decisionsFor(t, ctx, ti, ti.projectID, requestID))
	require.Equal(t, "requested", requestStatus(t, ctx, ti, ti.projectID, requestID))
}
