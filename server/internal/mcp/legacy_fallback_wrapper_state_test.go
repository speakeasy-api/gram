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
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

// The slug fallback serves a canonical pair only while the wrapper matches the toolset columns.
func TestServePublic_LegacySlugCanonicalWrapperState(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		visibility string
		serves     bool
	}{
		{name: "matching public wrapper serves anonymously", visibility: "public", serves: true},
		{name: "drifted private wrapper stays hidden", visibility: "private", serves: false},
		{name: "drifted disabled wrapper stays hidden", visibility: "disabled", serves: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMCPService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			require.NotNil(t, authCtx.ProjectID)

			slug := "legacy-alias-" + uuid.NewString()[:8]
			toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, slug)
			createEndpointlessCanonicalWrapper(t, ctx, ti, toolset, tt.visibility)

			w, err := servePublicHTTP(t, context.Background(), ti, slug, makeInitializeBody(), "", nil)
			if !tt.serves {
				require.Error(t, err)
				require.Contains(t, err.Error(), "not found")
				return
			}
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
		})
	}
}

// A hosted server whose endpoint moved keeps serving its old address through the toolset's legacy alias.
func TestServePublic_MovedEndpointKeepsLegacyAlias(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMCPService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	oldSlug := "legacy-old-" + uuid.NewString()[:8]
	newSlug := "moved-new-" + uuid.NewString()[:8]
	toolset := createPublicMCPToolset(t, ctx, toolsets_repo.New(ti.conn), authCtx, oldSlug)
	createEndpointlessCanonicalWrapper(t, ctx, ti, toolset, "public")
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
