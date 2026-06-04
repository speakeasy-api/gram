package variations_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
	repo "github.com/speakeasy-api/gram/server/internal/variations/repo"
)

// TestListByGroupIDAndToolURNs verifies the explicit-group variation lookup
// used by mv.ApplyVariations when an mcp_server or toolset pins a variation
// group: it returns only the requested group's matching rows.
func TestListByGroupIDAndToolURNs(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	r := repo.New(ti.conn)

	group, err := r.InitGlobalToolVariationsGroup(ctx, repo.InitGlobalToolVariationsGroupParams{
		ProjectID:   projectID,
		Name:        "group",
		Description: conv.ToPGText("group"),
	})
	require.NoError(t, err)

	toolURN, err := urn.ParseTool("tools:http:test:shared-tool")
	require.NoError(t, err)

	_, err = r.UpsertToolVariation(ctx, repo.UpsertToolVariationParams{
		GroupID:     group,
		SrcToolUrn:  toolURN,
		SrcToolName: "shared-tool",
		Name:        conv.ToPGText("variation"),
		Tags:        []string{"alpha", "beta"},
	})
	require.NoError(t, err)

	urns := []string{toolURN.String()}

	// Correct group resolves the variation with its tags intact.
	rows, err := r.ListByGroupIDAndToolURNs(ctx, repo.ListByGroupIDAndToolURNsParams{
		GroupID:   group,
		ProjectID: projectID,
		ToolUrns:  urns,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, group, rows[0].GroupID)
	require.Equal(t, []string{"alpha", "beta"}, rows[0].Tags)

	// A different group id (same project) matches nothing.
	rows, err = r.ListByGroupIDAndToolURNs(ctx, repo.ListByGroupIDAndToolURNsParams{
		GroupID:   uuid.New(),
		ProjectID: projectID,
		ToolUrns:  urns,
	})
	require.NoError(t, err)
	require.Empty(t, rows)

	// A tool URN with no variation in the group matches nothing.
	rows, err = r.ListByGroupIDAndToolURNs(ctx, repo.ListByGroupIDAndToolURNsParams{
		GroupID:   group,
		ProjectID: projectID,
		ToolUrns:  []string{"tools:http:test:other-tool"},
	})
	require.NoError(t, err)
	require.Empty(t, rows)
}

// TestUpsertToolVariation_TagsNilEmptySetRoundTrip locks down the three-state
// semantics the ?tags= MCP filter relies on: a variation's tags must survive a
// write/read round-trip distinguishing nil (no tag modification — source tags
// stay authoritative), a non-nil empty set (tool removed from every tag filter),
// and a populated set (replaces the source tags). The tool_variations.tags
// column is nullable with no default, and pgx maps SQL NULL <-> nil []string and
// '{}' <-> a non-nil empty []string; if that ever regressed, "unset" would
// silently collapse into "removed from all filters".
func TestUpsertToolVariation_TagsNilEmptySetRoundTrip(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	r := repo.New(ti.conn)

	group, err := r.InitGlobalToolVariationsGroup(ctx, repo.InitGlobalToolVariationsGroupParams{
		ProjectID:   projectID,
		Name:        "group",
		Description: conv.ToPGText("group"),
	})
	require.NoError(t, err)

	cases := []struct {
		name      string
		write     []string
		wantNil   bool
		wantValue []string
	}{
		{name: "nil-stays-nil", write: nil, wantNil: true},
		{name: "empty-stays-non-nil-empty", write: []string{}, wantNil: false, wantValue: []string{}},
		{name: "populated-round-trips", write: []string{"alpha", "beta"}, wantNil: false, wantValue: []string{"alpha", "beta"}},
	}

	for i, tc := range cases {
		toolURN, err := urn.ParseTool("tools:http:test:tags-roundtrip-" + uuid.New().String()[:8])
		require.NoError(t, err)

		written, err := r.UpsertToolVariation(ctx, repo.UpsertToolVariationParams{
			GroupID:     group,
			SrcToolUrn:  toolURN,
			SrcToolName: "tags-roundtrip",
			Tags:        tc.write,
		})
		require.NoError(t, err, "case %d (%s) write", i, tc.name)

		// Assert on the value read back through a SELECT, not just the RETURNING
		// row, so we exercise the read decode path the filter actually uses.
		rows, err := r.ListByGroupIDAndToolURNs(ctx, repo.ListByGroupIDAndToolURNsParams{
			GroupID:   group,
			ProjectID: projectID,
			ToolUrns:  []string{toolURN.String()},
		})
		require.NoError(t, err, "case %d (%s) read", i, tc.name)
		require.Len(t, rows, 1, "case %d (%s)", i, tc.name)

		for _, got := range [][]string{written.Tags, rows[0].Tags} {
			if tc.wantNil {
				require.Nil(t, got, "case %d (%s): nil tags must stay nil", i, tc.name)
			} else {
				require.NotNil(t, got, "case %d (%s): empty tags must stay non-nil", i, tc.name)
				require.Equal(t, tc.wantValue, got, "case %d (%s)", i, tc.name)
			}
		}
	}
}

// TestListByGroupIDAndToolURNs_ProjectScoped confirms a group id is not
// honored under a project that does not own it, preventing cross-project
// variation leakage.
func TestListByGroupIDAndToolURNs_ProjectScoped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestVariationsService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	projectID := *authCtx.ProjectID

	r := repo.New(ti.conn)

	group, err := r.InitGlobalToolVariationsGroup(ctx, repo.InitGlobalToolVariationsGroupParams{
		ProjectID:   projectID,
		Name:        "group",
		Description: conv.ToPGText("group"),
	})
	require.NoError(t, err)

	toolURN, err := urn.ParseTool("tools:http:test:scoped-tool")
	require.NoError(t, err)

	_, err = r.UpsertToolVariation(ctx, repo.UpsertToolVariationParams{
		GroupID:     group,
		SrcToolUrn:  toolURN,
		SrcToolName: "scoped-tool",
		Tags:        []string{"alpha"},
	})
	require.NoError(t, err)

	rows, err := r.ListByGroupIDAndToolURNs(ctx, repo.ListByGroupIDAndToolURNsParams{
		GroupID:   group,
		ProjectID: uuid.New(), // unrelated project
		ToolUrns:  []string{toolURN.String()},
	})
	require.NoError(t, err)
	require.Empty(t, rows)
}
