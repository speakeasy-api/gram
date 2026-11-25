package agent_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestListAiScanTargetsShowsABlockOnARemovedProductButAgentsNeverGetIt(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, ti.orgID))
	require.NoError(t, catalogseed.EnsureCatalog(ctx, ti.conn))
	_, err := repo.New(ti.conn).SetAIScanTargetStatus(ctx, repo.SetAIScanTargetStatusParams{
		OrganizationID: ti.orgID, ID: "zed", AiProductID: conv.ToPGText("zed"), Status: "blocked", Rationale: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	require.NoError(t, testrepo.New(ti.conn).EditProductFixture(ctx, testrepo.EditProductFixtureParams{
		ID: "zed", DisplayName: "Zed", DeletedAt: pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true, InfinityModifier: pgtype.Finite},
	}))
	snapshot, err := aivendors.Load(ctx, ti.conn, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	ti.service.SetAICatalog(aivendors.NewStore(snapshot))

	listed, err := ti.service.ListAiScanTargets(ctx, &gen.ListAiScanTargetsPayload{})
	require.NoError(t, err)
	var zed *gen.AiScanTarget
	for _, target := range listed.Targets {
		if target.ID == "zed" {
			zed = target
		}
	}
	require.NotNil(t, zed, "the admin can see the block that still applies")
	require.Equal(t, "Zed", zed.DisplayName)
	require.Equal(t, "blocked", zed.Access.State)
	require.Equal(t, "removed", conv.PtrValOr(zed.Access.CatalogStatus, ""))

	poll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.NotContains(t, servedTargetIDs(t, aiScanEnvelope(t, poll)), "zed", "device agents never probe for a removed product")
}
