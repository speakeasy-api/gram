package mv

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	tr "github.com/speakeasy-api/gram/server/internal/tools/repo"
	tsr "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var _ func(
	context.Context,
	*slog.Logger,
	*pgxpool.Pool,
	ProjectID,
	[]tsr.Toolset,
	...platformtools.ExternalTool,
) ([]*types.ToolsetEntry, error) = DescribeToolsetEntries

func TestValidateToolsetEntriesInputsRejectsMixedProjects(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	toolsets := []tsr.Toolset{
		{ID: uuid.New(), OrganizationID: "org", ProjectID: projectID},
		{ID: uuid.New(), OrganizationID: "org", ProjectID: uuid.New()},
	}

	err := validateToolsetEntriesInputs(ProjectID(projectID), toolsets)

	require.ErrorContains(t, err, "same project")
}

func TestSelectHTTPDefinitionsByToolsetPreservesNewestNameMatch(t *testing.T) {
	t.Parallel()

	toolsetA := uuid.New()
	toolsetB := uuid.New()
	toolURNA := urn.NewTool(urn.ToolKindHTTP, "source", "a")
	toolURNB := urn.NewTool(urn.ToolKindHTTP, "source", "b")

	newestA := tr.FindHttpToolEntriesByUrnRow{ID: uuid.New(), ToolUrn: toolURNA, Name: "shared-name"}
	olderA := tr.FindHttpToolEntriesByUrnRow{ID: uuid.New(), ToolUrn: toolURNA, Name: "shared-name"}
	newestB := tr.FindHttpToolEntriesByUrnRow{ID: uuid.New(), ToolUrn: toolURNB, Name: "shared-name"}
	distinctB := tr.FindHttpToolEntriesByUrnRow{ID: uuid.New(), ToolUrn: toolURNB, Name: "distinct-name"}

	selected := selectHTTPDefinitionsByToolset(
		map[uuid.UUID][]string{
			toolsetA: {toolURNA.String(), toolURNB.String()},
			toolsetB: {toolURNB.String()},
		},
		[]tr.FindHttpToolEntriesByUrnRow{newestA, olderA, newestB, distinctB},
	)

	require.Equal(t, []tr.FindHttpToolEntriesByUrnRow{newestA, distinctB}, selected[toolsetA])
	require.Equal(t, []tr.FindHttpToolEntriesByUrnRow{newestB, distinctB}, selected[toolsetB])
}

func TestGroupDefinitionsByToolsetURNPreservesDefinitionOrder(t *testing.T) {
	t.Parallel()

	type definition struct {
		URN string
	}

	toolsetID := uuid.New()
	definitions := []definition{{URN: "resource:newer"}, {URN: "resource:older"}}

	grouped := groupDefinitionsByToolsetURN(
		map[uuid.UUID][]string{toolsetID: {"resource:older", "resource:newer"}},
		definitions,
		func(def definition) string { return def.URN },
	)

	require.Equal(t, definitions, grouped[toolsetID])
}
