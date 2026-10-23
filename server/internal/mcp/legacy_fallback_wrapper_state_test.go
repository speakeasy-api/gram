package mcp_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// The slug fallback serves a toolset's frozen mcp_slug, but the canonical wrapper owns whether and how it serves.
func TestServePublic_LegacySlugFollowsCanonicalWrapperState(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		visibility string
		wantStatus int
	}{
		{name: "disabled wrapper hides a public toolset", visibility: "disabled", wantStatus: 0},
		{name: "private wrapper gates a public toolset", visibility: "private", wantStatus: http.StatusUnauthorized},
		{name: "public wrapper serves anonymously", visibility: "public", wantStatus: http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.ProjectID)

			slug := "frozen-alias-" + uuid.NewString()[:8]
			toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, slug)
			createEndpointlessCanonicalWrapper(t, ctx, ti, toolset, tt.visibility)

			w, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
			if tt.wantStatus == 0 {
				require.Error(t, err)
				require.Contains(t, err.Error(), "not found")
				return
			}
			if tt.wantStatus == http.StatusUnauthorized {
				require.Error(t, err, "anonymous request to a private server")
				require.Contains(t, err.Error(), "access token")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantStatus, w.Code, "body=%s", w.Body.String())
		})
	}
}

// A private toolset whose canonical wrapper went public serves anonymously through the fallback.
func TestServePublic_LegacySlugPrivateToolsetPublicWrapper(t *testing.T) {
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
		McpSlug:                pgtype.Text{String: slug, Valid: true},
		McpEnabled:             true,
	})
	require.NoError(t, err)

	_, err = servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
	require.Error(t, err, "private toolset without a wrapper requires auth")

	createEndpointlessCanonicalWrapper(t, ctx, ti, toolset, "public")

	w, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
}

func createEndpointlessCanonicalWrapper(t *testing.T, ctx context.Context, ti *testInstance, toolset toolsets_repo.Toolset, visibility string) {
	t.Helper()

	_, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    toolset.ID,
		ProjectID:             toolset.ProjectID,
		Name:                  pgtype.Text{String: toolset.Name, Valid: true},
		Slug:                  toolset.McpSlug,
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: toolset.ID, Valid: true},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            visibility,
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
}
