package mcp_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// mcpServerBySlug resolves the id a proxy-backed member's mcp:connect grant is
// keyed on. Grants for toolset-backed servers key on the toolset instead.
func mcpServerBySlug(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	server, err := mcpserversrepo.New(ti.conn).GetMCPServerBySlug(ctx, mcpserversrepo.GetMCPServerBySlugParams{
		Slug:      conv.ToPGText(slug),
		ProjectID: projectID,
	})
	require.NoError(t, err)
	return server.ID
}

func mintAgentKey(t *testing.T, ctx context.Context, ti *testInstance, agentID, resourceID uuid.UUID) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	return mintAgentKeyForProject(t, ctx, ti, agentID, *authCtx.ProjectID, resourceID)
}

func mintAgentKeyForProject(t *testing.T, ctx context.Context, ti *testInstance, agentID, projectID, resourceID uuid.UUID) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	policy, err := runtimepolicy.NewDelegatedPolicyV1([]authz.Grant{{
		Scope: authz.ScopeMCPConnect,
		Selector: authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   resourceID.String(),
			authz.SelectorKeyProjectID:    projectID.String(),
		},
	}})
	require.NoError(t, err)
	delegated, err := runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, policy)
	require.NoError(t, err)
	token, hash, prefix, err := auth.GenerateAPIKeyMaterial(auth.APIKeyPrefix("test"))
	require.NoError(t, err)
	_, err = keysrepo.New(ti.conn).CreateAgentAPIKey(ctx, keysrepo.CreateAgentAPIKeyParams{
		OrganizationID: authCtx.ActiveOrganizationID, CreatedByUserID: authCtx.UserID,
		Name: "agent key " + uuid.NewString()[:8], KeyPrefix: prefix, KeyHash: hash,
		SubjectUrn:             pgtype.Text{String: urn.NewAgentSubject(agentID).String(), Valid: true},
		DelegatedGrants:        delegated,
		DelegatedGrantsVersion: pgtype.Int4{Int32: int32(runtimepolicy.CurrentDelegatedPolicyVersion), Valid: true},
		ExpiresAt:              pgtype.Timestamptz{Time: time.Now().Add(time.Hour), InfinityModifier: pgtype.Finite, Valid: true},
	})
	require.NoError(t, err)
	return token
}
