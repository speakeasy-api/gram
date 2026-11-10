package mcp_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// The slug fallback serves a toolset's legacy alias, but the canonical wrapper owns whether and how it serves.
func TestServePublic_LegacySlugFollowsCanonicalWrapperState(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		visibility string
		wantStatus int
	}{
		{name: "disabled wrapper hides the alias", visibility: "disabled", wantStatus: 0},
		{name: "private wrapper gates the alias", visibility: "private", wantStatus: http.StatusUnauthorized},
		{name: "public wrapper serves anonymously", visibility: "public", wantStatus: http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.ProjectID)

			slug := "frozen-alias-" + uuid.NewString()[:8]
			toolset := createPublicMCPToolset(t, ctx, ti, authCtx, slug)
			testenv.SetHostedMCP(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, testenv.HostedMCP{Visibility: tt.visibility})

			w, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
			switch tt.wantStatus {
			case 0:
				require.Error(t, err)
				require.Contains(t, err.Error(), "not found")
			case http.StatusUnauthorized:
				require.Error(t, err, "anonymous request to a private server")
				require.Contains(t, err.Error(), "access token")
			default:
				require.NoError(t, err)
				require.Equal(t, tt.wantStatus, w.Code, "body=%s", w.Body.String())
			}
		})
	}
}

// A toolset without a canonical wrapper is not hosted, so its alias serves only once a wrapper exists.
func TestServePublic_LegacySlugWithoutWrapperNotServed(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	slug := "frozen-private-" + uuid.NewString()[:8]
	toolset, err := toolsets_repo.New(ti.conn).CreateToolset(ctx, toolsets_repo.CreateToolsetParams{
		OrganizationID:         authCtx.ActiveOrganizationID,
		ProjectID:              *authCtx.ProjectID,
		Name:                   "Frozen private " + slug,
		Slug:                   slug,
		Description:            pgtype.Text{String: "", Valid: false},
		DefaultEnvironmentSlug: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	testenv.SetLegacyAlias(t, ctx, ti.conn, toolset.ProjectID, toolset.ID, slug, uuid.NullUUID{UUID: uuid.Nil, Valid: false})

	_, err = servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
	require.Error(t, err, "toolset without a wrapper is not hosted")
	require.Contains(t, err.Error(), "not found")

	hostLegacyToolset(t, ctx, ti, toolset, slug, "public", noIssuer())

	w, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
}

// A hosted server whose endpoint moved keeps serving its old address through the frozen toolset alias.
func TestServePublic_MovedEndpointKeepsFrozenAlias(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	oldSlug := "frozen-old-" + uuid.NewString()[:8]
	newSlug := "moved-new-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, ti, authCtx, oldSlug)
	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: toolset.ProjectID, McpServerID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Slug: newSlug,
	})
	require.NoError(t, err)

	for _, slug := range []string{newSlug, oldSlug} {
		w, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
		require.NoError(t, err, slug)
		require.Equal(t, http.StatusOK, w.Code, "slug=%s body=%s", slug, w.Body.String())
	}
}
