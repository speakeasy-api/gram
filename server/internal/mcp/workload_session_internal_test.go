package mcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// workloadSessionCeilingFixture is one workload, one MCP server, and the
// issuer both belong to.
type workloadSessionCeilingFixture struct {
	endpoint *ResolvedMcpEndpoint
	subject  urn.SessionSubject
	version  runtimepolicy.DelegatedPolicyVersion
}

func newWorkloadSessionCeilingFixture() workloadSessionCeilingFixture {
	return workloadSessionCeilingFixture{
		endpoint: &ResolvedMcpEndpoint{
			OrganizationID:      uuid.NewString(),
			ProjectID:           uuid.New(),
			UserSessionIssuerID: uuid.New(),
			ToolsetID:           uuid.NullUUID{UUID: uuid.New(), Valid: true},
		},
		subject: urn.NewWorkloadSubject(uuid.New(), "repo:acme/payments-api:ref:refs/heads/main"),
		version: runtimepolicy.CurrentDelegatedPolicyVersion,
	}
}

func (f workloadSessionCeilingFixture) organization() pgtype.Text {
	return pgtype.Text{String: f.endpoint.OrganizationID, Valid: true}
}

func (f workloadSessionCeilingFixture) storedVersion() pgtype.Int4 {
	return pgtype.Int4{Int32: int32(f.version), Valid: true}
}

func (f workloadSessionCeilingFixture) singleServerCeiling(t *testing.T) []byte {
	t.Helper()

	target, ok := agentAuthorizationTarget(f.endpoint)
	require.True(t, ok)
	encoded, err := encodeAgentSessionPolicy(*target, f.version)
	require.NoError(t, err)
	return encoded
}

func (f workloadSessionCeilingFixture) issuerCeiling(t *testing.T, projectID uuid.UUID) []byte {
	t.Helper()

	encoded, err := encodeIssuerWorkloadSessionPolicy(projectID, f.version)
	require.NoError(t, err)
	return encoded
}

func (f workloadSessionCeilingFixture) gateway() *ResolvedMcpEndpoint {
	return &ResolvedMcpEndpoint{
		OrganizationID:      f.endpoint.OrganizationID,
		ProjectID:           f.endpoint.ProjectID,
		UserSessionIssuerID: f.endpoint.UserSessionIssuerID,
		RouteBase:           mcproute.MCP,
		Slug:                "gateway",
		MetaMcpServerID:     uuid.NullUUID{UUID: uuid.New(), Valid: true},
	}
}

func (f workloadSessionCeilingFixture) gatewayCeiling(t *testing.T, gateway *ResolvedMcpEndpoint) []byte {
	t.Helper()

	encoded, err := encodeGatewayWorkloadSessionPolicy(gateway.MetaMcpServerID.UUID, gateway.ProjectID, f.version)
	require.NoError(t, err)
	return encoded
}

// The issuer-wide ceiling grants mcp:connect on every MCP server in the
// issuer's project, or in its organization for an organization issuer.
func TestEncodeIssuerWorkloadSessionPolicy_WildcardWithinIssuerScope(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	cases := map[string]struct {
		projectID uuid.UUID
		want      authz.Selector
	}{
		"project issuer": {projectID: projectID, want: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    projectID.String(),
		}},
		"organization issuer": {projectID: uuid.Nil, want: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			version := runtimepolicy.CurrentDelegatedPolicyVersion
			encoded, err := encodeIssuerWorkloadSessionPolicy(tc.projectID, version)
			require.NoError(t, err)
			decoded, err := runtimepolicy.DecodeDelegatedPolicy(version, encoded)
			require.NoError(t, err)

			grants := decoded.RuntimeGrants()
			require.NotEmpty(t, grants)
			require.True(t, authz.GrantsContainSelector(grants, authz.ScopeMCPConnect, tc.want))
			// Only mcp:connect and the scopes it implies, so the ceiling never
			// carries authority beyond connecting to MCP servers.
			connectClosure := authz.ScopeImplicationClosure(authz.ScopeMCPConnect)
			for _, grant := range grants {
				require.Contains(t, connectClosure, grant.Scope)
				require.Equal(t, tc.want, grant.Selector)
			}
		})
	}
}

func TestLoadIssuerWorkloadSessionCredential_AcceptsItsOwnCeiling(t *testing.T) {
	t.Parallel()

	f := newWorkloadSessionCeilingFixture()
	_, err := loadIssuerWorkloadSessionCredential(f.endpoint.OrganizationID, f.endpoint.ProjectID, f.subject, f.subject, f.organization(), f.issuerCeiling(t, f.endpoint.ProjectID), f.storedVersion())
	require.NoError(t, err)
}

// A ceiling only rides the audience it was minted with: an issuer-wide
// audience never carries a single server's ceiling, nor one scoped to another
// project or organization.
func TestLoadIssuerWorkloadSessionCredential_RejectsAnyOtherCeiling(t *testing.T) {
	t.Parallel()

	f := newWorkloadSessionCeilingFixture()
	cases := map[string]struct {
		ceiling      []byte
		organization pgtype.Text
	}{
		"single server":        {ceiling: f.singleServerCeiling(t), organization: f.organization()},
		"another project":      {ceiling: f.issuerCeiling(t, uuid.New()), organization: f.organization()},
		"organization-wide":    {ceiling: f.issuerCeiling(t, uuid.Nil), organization: f.organization()},
		"another organization": {ceiling: f.issuerCeiling(t, f.endpoint.ProjectID), organization: pgtype.Text{String: uuid.NewString(), Valid: true}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := loadIssuerWorkloadSessionCredential(f.endpoint.OrganizationID, f.endpoint.ProjectID, f.subject, f.subject, tc.organization, tc.ceiling, f.storedVersion())
			require.Error(t, err)
		})
	}
}

// An organization issuer's ceiling spans its organization, with no project:
// that ceiling is accepted, and one scoped to any single project is not.
func TestLoadIssuerWorkloadSessionCredential_OrganizationIssuer(t *testing.T) {
	t.Parallel()

	f := newWorkloadSessionCeilingFixture()
	_, err := loadIssuerWorkloadSessionCredential(f.endpoint.OrganizationID, uuid.Nil, f.subject, f.subject, f.organization(), f.issuerCeiling(t, uuid.Nil), f.storedVersion())
	require.NoError(t, err)

	_, err = loadIssuerWorkloadSessionCredential(f.endpoint.OrganizationID, uuid.Nil, f.subject, f.subject, f.organization(), f.issuerCeiling(t, f.endpoint.ProjectID), f.storedVersion())
	require.Error(t, err)
}

// A session minted for one server never carries the issuer-wide ceiling, so a
// row edited to widen it authorizes nothing.
func TestLoadWorkloadSessionCredential_RejectsIssuerWideCeiling(t *testing.T) {
	t.Parallel()

	f := newWorkloadSessionCeilingFixture()
	_, err := loadWorkloadSessionCredential(f.endpoint, f.subject, f.subject, f.organization(), f.singleServerCeiling(t), f.storedVersion())
	require.NoError(t, err)

	_, err = loadWorkloadSessionCredential(f.endpoint, f.subject, f.subject, f.organization(), f.issuerCeiling(t, f.endpoint.ProjectID), f.storedVersion())
	require.Error(t, err)
}

func TestEncodeGatewayWorkloadSessionPolicy_NamesItsGateway(t *testing.T) {
	t.Parallel()

	f := newWorkloadSessionCeilingFixture()
	gateway := f.gateway()
	decoded, err := runtimepolicy.DecodeDelegatedPolicy(f.version, f.gatewayCeiling(t, gateway))
	require.NoError(t, err)

	project := gateway.ProjectID.String()
	wildcard := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: authz.WildcardResource, authz.SelectorKeyProjectID: project}
	named := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: gateway.MetaMcpServerID.UUID.String(), authz.SelectorKeyProjectID: project}
	grants := decoded.RuntimeGrants()
	require.True(t, authz.GrantsContainSelector(grants, authz.ScopeMCPConnect, wildcard))
	require.True(t, authz.GrantsContainSelector(grants, authz.ScopeMCPConnect, named))
	connectClosure := authz.ScopeImplicationClosure(authz.ScopeMCPConnect)
	for _, grant := range grants {
		require.Contains(t, connectClosure, grant.Scope)
		require.Contains(t, []authz.Selector{wildcard, named}, grant.Selector)
	}

	_, err = encodeGatewayWorkloadSessionPolicy(uuid.Nil, gateway.ProjectID, f.version)
	require.Error(t, err)
	_, err = encodeGatewayWorkloadSessionPolicy(gateway.MetaMcpServerID.UUID, uuid.Nil, f.version)
	require.Error(t, err)
}

func TestLoadWorkloadSessionCredential_GatewayAcceptsItsOwnCeiling(t *testing.T) {
	t.Parallel()

	f := newWorkloadSessionCeilingFixture()
	gateway := f.gateway()
	_, err := loadWorkloadSessionCredential(gateway, f.subject, f.subject, f.organization(), f.gatewayCeiling(t, gateway), f.storedVersion())
	require.NoError(t, err)
}

// "replaced gateway" is a gateway recreated at the same URL, issuer and
// project.
func TestLoadWorkloadSessionCredential_GatewayRejectsAnyOtherCeiling(t *testing.T) {
	t.Parallel()

	f := newWorkloadSessionCeilingFixture()
	gateway := f.gateway()
	replaced := *gateway
	replaced.MetaMcpServerID = uuid.NullUUID{UUID: uuid.New(), Valid: true}
	otherProject := *gateway
	otherProject.ProjectID = uuid.New()
	cases := map[string]struct {
		ceiling      []byte
		organization pgtype.Text
	}{
		"replaced gateway":     {ceiling: f.gatewayCeiling(t, &replaced), organization: f.organization()},
		"another project":      {ceiling: f.gatewayCeiling(t, &otherProject), organization: f.organization()},
		"issuer-wide":          {ceiling: f.issuerCeiling(t, gateway.ProjectID), organization: f.organization()},
		"organization-wide":    {ceiling: f.issuerCeiling(t, uuid.Nil), organization: f.organization()},
		"single server":        {ceiling: f.singleServerCeiling(t), organization: f.organization()},
		"another organization": {ceiling: f.gatewayCeiling(t, gateway), organization: pgtype.Text{String: uuid.NewString(), Valid: true}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := loadWorkloadSessionCredential(gateway, f.subject, f.subject, tc.organization, tc.ceiling, f.storedVersion())
			require.Error(t, err)
		})
	}
}

func TestLoadWorkloadSessionCredential_GatewayCeilingIsRefusedElsewhere(t *testing.T) {
	t.Parallel()

	f := newWorkloadSessionCeilingFixture()
	ceiling := f.gatewayCeiling(t, f.gateway())

	_, err := loadWorkloadSessionCredential(f.endpoint, f.subject, f.subject, f.organization(), ceiling, f.storedVersion())
	require.Error(t, err)
	_, err = loadIssuerWorkloadSessionCredential(f.endpoint.OrganizationID, f.endpoint.ProjectID, f.subject, f.subject, f.organization(), ceiling, f.storedVersion())
	require.Error(t, err)
}
