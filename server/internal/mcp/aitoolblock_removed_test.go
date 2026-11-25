package mcp_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed"
	seedrepo "github.com/speakeasy-api/gram/server/internal/aivendors/catalogseed/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

const zedCIMDClientID = "https://zed.dev/oauth/client-metadata.json"

var catalogRemovedAt = pgtype.Timestamptz{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true, InfinityModifier: pgtype.Finite}

// loadCatalogAfter seeds the catalog, applies a staff removal and loads the snapshot pods would serve.
func loadCatalogAfter(t *testing.T, ctx context.Context, ti *testInstance, remove func()) *aivendors.Snapshot {
	t.Helper()
	require.NoError(t, catalogseed.EnsureCatalog(ctx, ti.conn))
	remove()
	snapshot, err := aivendors.Load(ctx, ti.conn, testenv.NewLogger(t), aitargets.ValidateCatalog)
	require.NoError(t, err)
	return snapshot
}

func removeDocument(t *testing.T, ctx context.Context, ti *testInstance, url string, enabled bool, deleted pgtype.Timestamptz) {
	t.Helper()
	docs, err := seedrepo.New(ti.conn).ListDocuments(ctx)
	require.NoError(t, err)
	for _, d := range docs {
		if d.Url == url && !d.Deleted {
			require.NoError(t, testrepo.New(ti.conn).EditDocumentFixture(ctx, testrepo.EditDocumentFixtureParams{ID: d.ID, Url: d.Url, DisplayName: d.DisplayName, Enabled: enabled, DeletedAt: deleted}))
		}
	}
}

func TestCheckAIToolGatewayBlock_BlockSurvivesCatalogRemoval(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		clientID string
		remove   func(t *testing.T, ctx context.Context, ti *testInstance)
	}{
		{name: "soft-deleted product", clientID: zedCIMDClientID, remove: func(t *testing.T, ctx context.Context, ti *testInstance) {
			t.Helper()
			require.NoError(t, testrepo.New(ti.conn).EditProductFixture(ctx, testrepo.EditProductFixtureParams{ID: "zed", DisplayName: "Zed", DeletedAt: catalogRemovedAt}))
		}},
		{name: "disabled document", clientID: zedCIMDClientID, remove: func(t *testing.T, ctx context.Context, ti *testInstance) {
			t.Helper()
			removeDocument(t, ctx, ti, zedCIMDClientID, false, pgtype.Timestamptz{})
		}},
		{name: "soft-deleted document", clientID: zedCIMDClientID, remove: func(t *testing.T, ctx context.Context, ti *testInstance) {
			t.Helper()
			removeDocument(t, ctx, ti, zedCIMDClientID, true, catalogRemovedAt)
		}},
		{name: "soft-deleted pattern document", clientID: "https://pi.dev/oauth/fixture-server/client.json", remove: func(t *testing.T, ctx context.Context, ti *testInstance) {
			t.Helper()
			removeDocument(t, ctx, ti, "https://pi.dev/oauth/*/client.json", true, catalogRemovedAt)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			orgID := authCtx.ActiveOrganizationID
			product := "zed"
			if tc.name == "soft-deleted pattern document" {
				product = "pi"
			}
			snapshot := loadCatalogAfter(t, ctx, ti, func() { tc.remove(t, ctx, ti) })

			blockTarget(t, ctx, ti, orgID, "cursor")
			require.NoError(t, ti.service.CheckAIToolGatewayBlock(ctx, snapshot, orgID, tc.clientID), "a block on another tool does not reach a removed client")

			blockTarget(t, ctx, ti, orgID, product)
			err := ti.service.CheckAIToolGatewayBlock(ctx, snapshot, orgID, tc.clientID)
			blockedErr, isBlocked := errors.AsType[*mcp.AIToolBlockedError](err)
			require.True(t, isBlocked, "%v", err)
			require.Equal(t, product, blockedErr.TargetID)
			require.NotEmpty(t, blockedErr.DisplayName)

			_, err = agentrepo.New(ti.conn).SetAIScanTargetStatus(ctx, agentrepo.SetAIScanTargetStatusParams{
				OrganizationID: orgID, ID: product, Status: "approved", Rationale: conv.ToPGTextEmpty(""),
			})
			require.NoError(t, err)
			require.NoError(t, ti.service.CheckAIToolGatewayBlock(ctx, snapshot, orgID, tc.clientID), "clearing the block lets the client in again")
		})
	}
}

func TestCheckAIToolGatewayBlock_ReaddedURLFollowsTheNewProduct(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	snapshot := loadCatalogAfter(t, ctx, ti, func() {
		removeDocument(t, ctx, ti, zedCIMDClientID, true, catalogRemovedAt)
		require.NoError(t, testrepo.New(ti.conn).InsertDocumentFixture(ctx, testrepo.InsertDocumentFixtureParams{ID: uuid.New(), AiProductID: "goose", Url: zedCIMDClientID, DisplayName: "Goose", Enabled: true, IsPattern: false}))
	})

	blockTarget(t, ctx, ti, orgID, "zed")
	require.NoError(t, ti.service.CheckAIToolGatewayBlock(ctx, snapshot, orgID, zedCIMDClientID), "the old product's block no longer applies")

	blockTarget(t, ctx, ti, orgID, "goose")
	err := ti.service.CheckAIToolGatewayBlock(ctx, snapshot, orgID, zedCIMDClientID)
	blockedErr, isBlocked := errors.AsType[*mcp.AIToolBlockedError](err)
	require.True(t, isBlocked, "%v", err)
	require.Equal(t, "goose", blockedErr.TargetID)
}

func TestCheckAIToolGatewayBlock_AllowedClientsUnaffected(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	snapshot := loadCatalogAfter(t, ctx, ti, func() {
		require.NoError(t, testrepo.New(ti.conn).EditProductFixture(ctx, testrepo.EditProductFixtureParams{ID: "zed", DisplayName: "Zed", DeletedAt: catalogRemovedAt}))
	})
	blockTarget(t, ctx, ti, orgID, "zed")
	for _, clientID := range []string{claudeCodeCIMDClientID, codexCIMDClientID, "https://unknown.example.com/client.json", "opaque-dcr-client"} {
		require.NoError(t, ti.service.CheckAIToolGatewayBlock(ctx, snapshot, orgID, clientID), clientID)
	}
}

func TestCheckAIToolGatewayBlock_OrganizationTargetDecidesARemovedID(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	snapshot := loadCatalogAfter(t, ctx, ti, func() {
		require.NoError(t, testrepo.New(ti.conn).EditProductFixture(ctx, testrepo.EditProductFixtureParams{ID: "zed", DisplayName: "Zed", DeletedAt: catalogRemovedAt}))
	})
	// The organization's own target under the removed id claims other clients only.
	_, err := agentrepo.New(ti.conn).UpsertAIScanTarget(ctx, agentrepo.UpsertAIScanTargetParams{
		OrganizationID: orgID, ID: "zed", DisplayName: conv.ToPGText("Our Zed"), Category: conv.ToPGText("harness"),
		BundleIds: []string{}, Binaries: []string{"zed"}, ConfigDirs: []string{}, ProcessNames: []string{},
		VersionPlistKey: pgtype.Text{}, CimdVendorKeys: []string{}, OauthClientIds: []string{"https://example.com/our-zed.json"}, ClientInfoNames: []string{},
	})
	require.NoError(t, err)
	blockTarget(t, ctx, ti, orgID, "zed")
	require.NoError(t, ti.service.CheckAIToolGatewayBlock(ctx, snapshot, orgID, zedCIMDClientID))
}

// TestHandleToken_BlockSurvivesProductRemoval runs the real token endpoint on a
// catalog that no longer lists the blocked product.
func TestHandleToken_BlockSurvivesProductRemoval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	snapshot := loadCatalogAfter(t, ctx, ti, func() {
		require.NoError(t, testrepo.New(ti.conn).EditProductFixture(ctx, testrepo.EditProductFixtureParams{ID: "claude-code", DisplayName: "Claude Code", DeletedAt: catalogRemovedAt}))
	})
	_, known := snapshot.CatalogPreset(claudeCodeCIMDClientID)
	require.False(t, known)
	ti.service.SetAICatalog(aivendors.NewStore(snapshot))

	client := seedCIMDClient(t, ctx, ti, issuer.ID, claudeCodeCIMDClientID)
	code, verifier := seedAuthorizationCode(t, ctx, ti, toolset, client)
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "claude-code")

	w := postForm(t, ti, toolset.McpSlug.String, "token", codeGrantForm(client, code, verifier))

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireTokenOAuthError(t, w, "unauthorized_client")
	requireTokenErrorDescription(t, w, "Claude Code")
}

func TestCheckAIToolGatewayBlock_OrganizationTargetKeepsCatalogMatchersAfterRemoval(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		clientID string
		gateway  func() ([]string, []string)
		remove   func(t *testing.T, ctx context.Context, ti *testInstance)
	}{
		{name: "vendor key", clientID: zedCIMDClientID, gateway: func() ([]string, []string) { return []string{"zed"}, []string{} }, remove: func(t *testing.T, ctx context.Context, ti *testInstance) {
			t.Helper()
			require.NoError(t, testrepo.New(ti.conn).EditProductFixture(ctx, testrepo.EditProductFixtureParams{ID: "zed", DisplayName: "Zed", DeletedAt: catalogRemovedAt}))
		}},
		{name: "catalog pattern", clientID: "https://pi.dev/oauth/fixture-server/client.json", gateway: func() ([]string, []string) { return []string{}, []string{"https://pi.dev/oauth/*/client.json"} }, remove: func(t *testing.T, ctx context.Context, ti *testInstance) {
			t.Helper()
			removeDocument(t, ctx, ti, "https://pi.dev/oauth/*/client.json", true, catalogRemovedAt)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			orgID := authCtx.ActiveOrganizationID
			snapshot := loadCatalogAfter(t, ctx, ti, func() { tc.remove(t, ctx, ti) })
			vendorKeys, clientIDs := tc.gateway()
			_, err := agentrepo.New(ti.conn).UpsertAIScanTarget(ctx, agentrepo.UpsertAIScanTargetParams{
				OrganizationID: orgID, ID: "our-tool", DisplayName: conv.ToPGText("Our tool"), Category: conv.ToPGText("harness"),
				BundleIds: []string{}, Binaries: []string{"our-tool"}, ConfigDirs: []string{}, ProcessNames: []string{},
				VersionPlistKey: pgtype.Text{}, CimdVendorKeys: vendorKeys, OauthClientIds: clientIDs, ClientInfoNames: []string{},
			})
			require.NoError(t, err)
			blockTarget(t, ctx, ti, orgID, "our-tool")

			err = ti.service.CheckAIToolGatewayBlock(ctx, snapshot, orgID, tc.clientID)
			blockedErr, isBlocked := errors.AsType[*mcp.AIToolBlockedError](err)
			require.True(t, isBlocked, "%v", err)
			require.Equal(t, "our-tool", blockedErr.TargetID)
		})
	}
}

func TestHandleToken_RefreshRefusedAfterProductRemoval(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPServiceWithIdentityResolver(t, &mockIdentityResolver{})
	toolset, issuer, _ := seedPrivateToolsetWithIssuer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	snapshot := loadCatalogAfter(t, ctx, ti, func() {
		require.NoError(t, testrepo.New(ti.conn).EditProductFixture(ctx, testrepo.EditProductFixtureParams{ID: "claude-code", DisplayName: "Claude Code", DeletedAt: catalogRemovedAt}))
	})
	ti.service.SetAICatalog(aivendors.NewStore(snapshot))

	client := seedCIMDClient(t, ctx, ti, issuer.ID, claudeCodeCIMDClientID)
	blockTarget(t, ctx, ti, authCtx.ActiveOrganizationID, "claude-code")

	w := postForm(t, ti, toolset.McpSlug.String, "token", url.Values{"grant_type": {"refresh_token"}, "client_id": {client.ClientID}, "refresh_token": {"unused-refresh-token"}})

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	requireTokenOAuthError(t, w, "invalid_grant")
	requireTokenErrorDescription(t, w, "administrator must approve")
}

func TestCheckAIToolGatewayBlock_RemovedSiblingDoesNotInheritAVendorBlock(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	orgID := authCtx.ActiveOrganizationID
	snapshot := loadCatalogAfter(t, ctx, ti, func() {
		require.NoError(t, testrepo.New(ti.conn).EditProductFixture(ctx, testrepo.EditProductFixtureParams{ID: "claude-code", DisplayName: "Claude Code", DeletedAt: catalogRemovedAt}))
	})
	// Aimed at the vendor's live products, not at the removed one.
	_, err := agentrepo.New(ti.conn).UpsertAIScanTarget(ctx, agentrepo.UpsertAIScanTargetParams{
		OrganizationID: orgID, ID: "our-anthropic", DisplayName: conv.ToPGText("Our Anthropic"), Category: conv.ToPGText("assistant"),
		BundleIds: []string{}, Binaries: []string{"our-anthropic"}, ConfigDirs: []string{}, ProcessNames: []string{},
		VersionPlistKey: pgtype.Text{}, CimdVendorKeys: []string{"anthropic"}, OauthClientIds: []string{}, ClientInfoNames: []string{},
	})
	require.NoError(t, err)
	blockTarget(t, ctx, ti, orgID, "our-anthropic")
	require.NoError(t, ti.service.CheckAIToolGatewayBlock(ctx, snapshot, orgID, claudeCodeCIMDClientID))
}
