package agent_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/aivendors/aivendorstest"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

// registryScanListGolden is the served list recorded from the registry-backed
// defaults, for an organization with no rows.
type registryScanListGolden struct {
	// ListVersion is the served list_version.
	ListVersion int32 `json:"list_version"`

	// ETag is the served ETag.
	ETag string `json:"etag"`

	// Envelope is the ai_scan envelope agents receive.
	Envelope map[string]any `json:"envelope"`
}

func readRegistryScanListGolden(t *testing.T) registryScanListGolden {
	t.Helper()
	data, err := os.ReadFile("aitargets/testdata/defaults_golden.json")
	require.NoError(t, err)
	var golden registryScanListGolden
	require.NoError(t, json.Unmarshal(data, &golden))
	return golden
}

func TestOrganizationWithoutRowsIsServedTheRegistryScanList(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	golden := readRegistryScanListGolden(t)

	list, err := aitargets.LoadOrganizationList(ctx, repo.New(ti.conn), ti.orgID, aivendorstest.CompiledSnapshot())
	require.NoError(t, err)
	require.Equal(t, golden.ListVersion, list.Snapshot.ListVersion)
	require.Equal(t, golden.ETag, list.Snapshot.ETag)

	poll, err := ti.service.GetPlugins(ctx, &gen.GetPluginsPayload{Email: new("developer@example.com")})
	require.NoError(t, err)
	require.Equal(t, golden.Envelope, aiScanEnvelope(t, poll))
}

// A decision-only row under a built-in id with no product link still resolves
// as that built-in: the decision applies and the served list is unchanged.
func TestUnlinkedDecisionRowResolvesAsTheBuiltIn(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestAgentService(t)
	golden := readRegistryScanListGolden(t)
	catalog := aivendorstest.CompiledSnapshot()

	builtin, ok := aitargets.DefaultByID(catalog, "aider")
	require.True(t, ok)
	row, err := repo.New(ti.conn).SetAIScanTargetStatus(ctx, repo.SetAIScanTargetStatusParams{
		OrganizationID: ti.orgID,
		ID:             builtin.ID,
		AiProductID:    pgtype.Text{String: "", Valid: false},
		Status:         "blocked",
		Rationale:      conv.ToPGTextEmpty("not approved"),
	})
	require.NoError(t, err)
	require.False(t, row.AiProductID.Valid, "the row must carry no product link")

	list, err := aitargets.LoadOrganizationList(ctx, repo.New(ti.conn), ti.orgID, catalog)
	require.NoError(t, err)
	entry, ok := list.Entry(builtin.ID)
	require.True(t, ok)
	require.Equal(t, aitargets.SourceDefault, entry.Source)
	require.True(t, entry.Customized)
	require.Equal(t, builtin, entry.Target)
	require.EqualValues(t, "blocked", entry.Decision.Decision)
	require.Equal(t, "not approved", entry.Decision.Rationale)
	require.Equal(t, golden.ETag, list.Snapshot.ETag, "a decision does not change the served definitions")
}
