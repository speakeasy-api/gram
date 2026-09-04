package mcpendpoints_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpendpoints"
	"github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
)

func endpointFixture(slug string, domainID uuid.NullUUID, domainRoot bool, createdAt time.Time) repo.McpEndpoint {
	return repo.McpEndpoint{
		ID:             uuid.New(),
		Slug:           slug,
		CustomDomainID: domainID,
		IsDomainRoot:   pgtype.Bool{Bool: domainRoot, Valid: true},
		CreatedAt:      pgtype.Timestamptz{Time: createdAt, Valid: true},
	}
}

func TestPrimaryEndpoint_Ordering(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	domain := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	platform := endpointFixture("platform", uuid.NullUUID{}, false, base)
	olderCustom := endpointFixture("older-custom", domain, false, base.Add(time.Hour))
	newerRoot := endpointFixture("newer-root", domain, true, base.Add(2*time.Hour))

	require.Nil(t, mcpendpoints.PrimaryEndpoint(nil))

	got := mcpendpoints.PrimaryEndpoint([]repo.McpEndpoint{platform, olderCustom})
	require.Equal(t, "older-custom", got.Slug, "custom domain outranks platform")

	got = mcpendpoints.PrimaryEndpoint([]repo.McpEndpoint{platform, olderCustom, newerRoot})
	require.Equal(t, "newer-root", got.Slug, "domain root outranks older non-root")

	oldPlatform := endpointFixture("old-platform", uuid.NullUUID{}, false, base)
	newPlatform := endpointFixture("new-platform", uuid.NullUUID{}, false, base.Add(time.Minute))
	got = mcpendpoints.PrimaryEndpoint([]repo.McpEndpoint{newPlatform, oldPlatform})
	require.Equal(t, "old-platform", got.Slug, "earlier creation wins within a rank")

	tieA := endpointFixture("tie-a", uuid.NullUUID{}, false, base)
	tieB := endpointFixture("tie-b", uuid.NullUUID{}, false, base)
	want := tieA
	if tieB.ID.String() < tieA.ID.String() {
		want = tieB
	}
	got = mcpendpoints.PrimaryEndpoint([]repo.McpEndpoint{tieA, tieB})
	require.Equal(t, want.ID, got.ID, "equal created_at breaks ties by id")
	got = mcpendpoints.PrimaryEndpoint([]repo.McpEndpoint{tieB, tieA})
	require.Equal(t, want.ID, got.ID, "tie-break is order-independent")
}

func TestEndpointURL(t *testing.T) {
	t.Parallel()

	domain := uuid.NullUUID{UUID: uuid.New(), Valid: true}
	root := endpointFixture("root", domain, true, time.Now())
	custom := endpointFixture("custom", domain, false, time.Now())
	platform := endpointFixture("platform", uuid.NullUUID{}, false, time.Now())

	u, err := mcpendpoints.EndpointURL(&root, "mcp.example.com", "http://0.0.0.0")
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com", u)

	u, err = mcpendpoints.EndpointURL(&custom, "mcp.example.com", "http://0.0.0.0")
	require.NoError(t, err)
	require.Equal(t, "https://mcp.example.com/mcp/custom", u)

	u, err = mcpendpoints.EndpointURL(&platform, "", "http://0.0.0.0/")
	require.NoError(t, err)
	require.Equal(t, "http://0.0.0.0/mcp/platform", u, "trailing slash on serverURL does not double the separator")
}
