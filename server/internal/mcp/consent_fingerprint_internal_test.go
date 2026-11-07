package mcp

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authenforcement"
	"github.com/speakeasy-api/gram/server/internal/mcp/metamcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	productfeaturesrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
)

// hosted adds a toolset-backed server to the fixture's project.
func (f *authClientFixture) hosted() uuid.UUID {
	f.t.Helper()
	toolsetID, id := uuid.New(), uuid.New()
	require.NoError(f.t, f.q.SeedToolsetFixture(f.t.Context(), authclientbackfill.SeedToolsetFixtureParams{ID: toolsetID, OrganizationID: f.orgID, ProjectID: f.projectID, Name: "Hosted " + id.String()[:8], Slug: "h-" + id.String()[:8]}))
	require.NoError(f.t, f.q.SeedHostedMCPServerFixture(f.t.Context(), authclientbackfill.SeedHostedMCPServerFixtureParams{ID: id, ProjectID: f.projectID, Slug: pgtype.Text{String: "h-" + id.String()[:8], Valid: true}, ToolsetID: validUUID(toolsetID)}))
	return id
}

func TestCatalogMemberConfigurationsTypedHashIgnoresDerivedIssuer(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_consent_fingerprint_catalog")
	require.NoError(t, err)

	// Stamping the issuer stands in for the binding resync.
	f := newAuthClientFixture(t, pool)
	hosted := f.hosted()
	ids := []uuid.UUID{f.serverID, hosted}
	before, err := f.service.catalogMemberConfigurations(t.Context(), f.orgID, ids)
	require.NoError(t, err)
	require.NotEmpty(t, before[f.serverID].hash)
	require.NotEmpty(t, before[f.serverID].typedHash)

	require.NoError(t, f.q.SetServerRemoteSessionIssuerFixture(t.Context(), authclientbackfill.SetServerRemoteSessionIssuerFixtureParams{RemoteSessionIssuerID: validUUID(f.issuerID), ID: f.serverID}))
	require.NoError(t, f.q.SetServerRemoteSessionIssuerFixture(t.Context(), authclientbackfill.SetServerRemoteSessionIssuerFixtureParams{RemoteSessionIssuerID: validUUID(f.issuerID), ID: hosted}))
	after, err := f.service.catalogMemberConfigurations(t.Context(), f.orgID, ids)
	require.NoError(t, err)

	require.NotEqual(t, before[f.serverID].hash, after[f.serverID].hash, "the full fingerprint covers the derived authorization server")
	require.Equal(t, before[f.serverID].typedHash, after[f.serverID].typedHash, "a proxied server's typed fingerprint leaves it out")
	require.NotEqual(t, before[f.serverID].hash, before[f.serverID].typedHash)

	require.NotEqual(t, before[hosted].typedHash, after[hosted].typedHash, "a hosted server is routed by its derived authorization server")
	require.Equal(t, after[hosted].hash, after[hosted].typedHash)
}

func TestConsentBackendFingerprintFollowsEnforcement(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_consent_fingerprint_reuse")
	require.NoError(t, err)
	ctx := t.Context()

	t.Run("upstream client choice covers the derived issuer", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID)}
		before, err := f.service.consentBackendFingerprint(ctx, endpoint)
		require.NoError(t, err)
		require.NoError(t, f.q.SetServerRemoteSessionIssuerFixture(t.Context(), authclientbackfill.SetServerRemoteSessionIssuerFixtureParams{RemoteSessionIssuerID: validUUID(f.issuerID), ID: f.serverID}))
		after, err := f.service.consentBackendFingerprint(ctx, endpoint)
		require.NoError(t, err)
		require.NotEqual(t, before, after)
	})

	t.Run("typed auth clients leave a proxied server's derived issuer out", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(f.serverID)}
		issuerBound, err := f.service.consentBackendFingerprint(ctx, endpoint)
		require.NoError(t, err)

		f.enforce()
		typed, err := f.service.consentBackendFingerprint(ctx, endpoint)
		require.NoError(t, err)
		require.NotEqual(t, issuerBound, typed)
		recorded, err := f.service.consentBackendEvidence(ctx, endpoint)
		require.NoError(t, err)
		require.Equal(t, typed, recorded.typedHash)
		require.Equal(t, issuerBound, recorded.hash, "the issuer-covering evidence recorded beside it is what the upstream client choice compares")

		require.NoError(t, f.q.SetServerRemoteSessionIssuerFixture(t.Context(), authclientbackfill.SetServerRemoteSessionIssuerFixtureParams{RemoteSessionIssuerID: validUUID(f.issuerID), ID: f.serverID}))
		resynced, err := f.service.consentBackendFingerprint(ctx, endpoint)
		require.NoError(t, err)
		require.Equal(t, typed, resynced)

		_, err = productfeaturesrepo.New(pool).DeleteFeature(ctx, productfeaturesrepo.DeleteFeatureParams{OrganizationID: f.orgID, FeatureName: string(productfeatures.FeatureUpstreamAuthEnforced)})
		require.NoError(t, err)
		f.service.authEnforcement = authenforcement.NewGate(pool)
		rolledBack, err := f.service.consentBackendFingerprint(ctx, endpoint)
		require.NoError(t, err)
		require.NotEqual(t, issuerBound, rolledBack, "after enforcement ends the resynchronized issuer is a configuration change again")
	})

	t.Run("typed auth clients still cover a hosted server's derived issuer", func(t *testing.T) {
		t.Parallel()
		f := newAuthClientFixture(t, pool)
		f.enforce()
		hosted := f.hosted()
		endpoint := &ResolvedMcpEndpoint{OrganizationID: f.orgID, ProjectID: f.projectID, McpServerID: validUUID(hosted)}
		before, err := f.service.consentBackendFingerprint(ctx, endpoint)
		require.NoError(t, err)
		require.NoError(t, f.q.SetServerRemoteSessionIssuerFixture(t.Context(), authclientbackfill.SetServerRemoteSessionIssuerFixtureParams{RemoteSessionIssuerID: validUUID(f.issuerID), ID: hosted}))
		after, err := f.service.consentBackendFingerprint(ctx, endpoint)
		require.NoError(t, err)
		require.NotEqual(t, before, after)
	})
}

// The comparison follows the organization's enforcement, whichever gate the
// request arrived through: a gate with no member choices is judged the same.
func TestValidateCatalogMemberComparesTypedFingerprintUnderEnforcement(t *testing.T) {
	t.Parallel()
	pool, err := TestInfra.CloneTestDatabase(t, "mcp_consent_fingerprint_validate")
	require.NoError(t, err)

	const (
		approvedFull  = "approved full fingerprint"
		approvedTyped = "approved typed fingerprint"
		changed       = "changed fingerprint"
	)
	for _, tc := range []struct {
		name     string
		enforced bool
		approved connectionCatalogMember
		current  memberConfiguration
		rejected bool
	}{
		{name: "enforced ignores a resynchronized authorization server", enforced: true, approved: connectionCatalogMember{BackendHash: approvedFull, TypedBackendHash: approvedTyped}, current: memberConfiguration{hash: changed, typedHash: approvedTyped}},
		{name: "enforced still detects a changed configuration", enforced: true, approved: connectionCatalogMember{BackendHash: approvedFull, TypedBackendHash: approvedTyped}, current: memberConfiguration{hash: changed, typedHash: changed}, rejected: true},
		{name: "enforced approval without the typed fingerprint compares the full one", enforced: true, approved: connectionCatalogMember{BackendHash: approvedFull}, current: memberConfiguration{hash: approvedFull, typedHash: approvedTyped}},
		{name: "enforced approval without the typed fingerprint detects a resync", enforced: true, approved: connectionCatalogMember{BackendHash: approvedFull}, current: memberConfiguration{hash: changed, typedHash: approvedTyped}, rejected: true},
		{name: "after enforcement a resynchronized authorization server is a change", approved: connectionCatalogMember{BackendHash: approvedFull, TypedBackendHash: approvedTyped}, current: memberConfiguration{hash: changed, typedHash: approvedTyped}, rejected: true},
		{name: "after enforcement an approval made under it still holds", approved: connectionCatalogMember{BackendHash: approvedFull, TypedBackendHash: approvedTyped}, current: memberConfiguration{hash: approvedFull, typedHash: approvedTyped}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newAuthClientFixture(t, pool)
			if tc.enforced {
				f.enforce()
			}
			member := metaMember{serverID: f.serverID, projectID: f.projectID, slug: "source", backend: metaMemberBackendHosted, configuration: tc.current}
			approved := tc.approved
			approved.ServerID, approved.ProjectID = member.serverID, member.projectID
			gate := &metaGateContext{
				organizationID: f.orgID, discoveryMode: metamcp.DiscoveryModeCode, execute: true,
				catalogRef: &toolfilter.CatalogReference{Mode: toolfilter.CatalogModeCached},
				catalog:    &connectionCatalog{ID: uuid.New(), Members: map[string]connectionCatalogMember{member.slug: approved}},
			}
			var logs bytes.Buffer
			f.service.logger = slog.New(slog.NewJSONHandler(&logs, nil))
			err := f.service.validateCatalogMember(t.Context(), gate, member)
			if !tc.rejected {
				require.NoError(t, err)
				return
			}
			var consent catalogConsentError
			require.ErrorAs(t, err, &consent)
			require.ErrorContains(t, err, "configuration changed")
			var diagnostic map[string]any
			require.NoError(t, json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &diagnostic))
			comparisons, ok := diagnostic[string(attr.McpConsentComparisonsKey)].(map[string]any)
			require.True(t, ok)
			require.Equal(t, tc.enforced && tc.approved.TypedBackendHash != "", comparisons["typed_compared"])
		})
	}
}
