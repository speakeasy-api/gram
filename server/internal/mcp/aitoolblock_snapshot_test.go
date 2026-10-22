package mcp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	seedrepo "github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestAIToolBlockDoesNotExpandExistingVendorMatchers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		products int
		blocked  bool
	}{
		{name: "unique vendor", products: 1, blocked: true},
		{name: "vendor now has two products", products: 2, blocked: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			products := []aivendors.Product{
				{ID: "first", VendorKey: "shared", Documents: []aivendors.Document{{URL: "https://first.example.com/client.json", Enabled: true}}},
				{ID: "second", VendorKey: "shared", Documents: []aivendors.Document{{URL: "https://second.example.com/client.json", Enabled: true}}},
			}
			snapshot := aivendors.NewSnapshot(products[:tc.products])
			store := aivendors.NewStore(snapshot)
			service := &Service{aiCatalog: store, aiToolBlockReads: aiToolBlockReads{
				blockedTargetIDs: func(context.Context, *agentrepo.Queries, string) ([]string, error) { return []string{"custom"}, nil },
				catalog: func(_ context.Context, _ *agentrepo.Queries, _ string, captured *aivendors.Snapshot) (*aitargets.OrganizationList, error) {
					require.Same(t, snapshot, captured)
					targets := []aitargets.Target{{ID: "custom", DisplayName: "Custom target", GatewayClient: aitargets.GatewayClient{CIMDVendorKeys: []string{"shared"}}}}
					return &aitargets.OrganizationList{Entries: []aitargets.Entry{{Target: targets[0]}}, Snapshot: aitargets.NewSnapshot(captured.Version(), targets, captured.Fingerprint())}, nil
				},
			}}
			err := service.checkAIToolGatewayBlock(t.Context(), testenv.NewLogger(t), "test-org", "https://first.example.com/client.json")
			if tc.blocked {
				var blocked *AIToolBlockedError
				require.ErrorAs(t, err, &blocked)
			} else {
				require.ErrorIs(t, err, ErrAIToolBlockCheckUnavailable)
			}
		})
	}
}

func TestAIToolBlockRefusesCatalogMatcherCollisions(t *testing.T) {
	t.Parallel()
	db, err := TestInfra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	logger := testenv.NewLogger(t)
	initial, err := aivendors.Load(t.Context(), db, logger, aitargets.ValidateCatalog)
	require.NoError(t, err)
	store := aivendors.NewStore(initial)
	organizationID := "org_" + uuid.NewString()
	_, err = organizationsrepo.New(db).UpsertOrganizationMetadata(t.Context(), organizationsrepo.UpsertOrganizationMetadataParams{
		ID: organizationID, Name: "Catalog collision fixture", Slug: "collision-" + uuid.NewString(),
	})
	require.NoError(t, err)
	const clientID = "https://fixture.example.com/client.json"
	custom := aitargets.Target{
		ID: "z-custom", DisplayName: "Custom fixture", Category: aitargets.CategoryHarness,
		Signatures:    aitargets.Signatures{Binaries: []string{"fixture"}},
		GatewayClient: aitargets.GatewayClient{OAuthClientIDs: []string{clientID}},
	}
	require.NoError(t, aitargets.ValidateTarget(initial, custom))
	queries := agentrepo.New(db)
	_, err = queries.UpsertAIScanTarget(t.Context(), aitargets.UpsertParams(organizationID, custom))
	require.NoError(t, err)
	_, err = queries.SetAIScanTargetStatus(t.Context(), agentrepo.SetAIScanTargetStatusParams{
		OrganizationID: organizationID, ID: custom.ID, Status: "blocked",
	})
	require.NoError(t, err)
	service := &Service{db: db, aiCatalog: store, aiToolBlockReads: defaultAIToolBlockReads()}
	var blocked *AIToolBlockedError
	require.ErrorAs(t, service.checkAIToolGatewayBlock(t.Context(), logger, organizationID, clientID), &blocked)
	require.Equal(t, custom.ID, blocked.TargetID)

	// This valid catalog addition sorts before the existing blocked custom target.
	seed := seedrepo.New(db)
	require.NoError(t, seed.InsertProduct(t.Context(), seedrepo.InsertProductParams{
		ID: "a-added", VendorKey: "fixture", DisplayName: "Added fixture", Category: pgtype.Text{String: "harness", Valid: true},
		BundleIds: []string{}, Binaries: []string{"fixture"}, ConfigDirs: []string{}, ProcessNames: []string{}, ClientInfoNames: []string{},
	}))
	require.NoError(t, seed.InsertDocument(t.Context(), seedrepo.InsertDocumentParams{
		ID: uuid.New(), AiProductID: "a-added", Url: clientID, DisplayName: "Added fixture", Enabled: true,
	}))
	require.NoError(t, store.Refresh(t.Context(), db, logger, aitargets.ValidateCatalog))
	err = service.checkAIToolGatewayBlock(t.Context(), logger, organizationID, clientID)
	require.ErrorIs(t, err, ErrAIToolBlockCheckUnavailable)
	require.ErrorContains(t, err, "both claim the OAuth client id")
	list, err := aitargets.LoadOrganizationList(t.Context(), queries, organizationID, store.Snapshot())
	require.ErrorIs(t, err, aitargets.ErrInvalidTarget)
	require.Nil(t, list, "a conflicting overlay must not become a device configuration")
}
