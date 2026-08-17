package exposure_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/exposure"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// fakeReader stands in for the ClickHouse-backed telemetry repo, recording what
// it was asked so tenancy and key handling can be asserted.
type fakeReader struct {
	row      *telemetryrepo.ShadowMCPInventoryURLRow
	usage    []telemetryrepo.ShadowMCPInventoryUsageRow
	rowErr   error
	usageErr error

	gotInventory []telemetryrepo.GetShadowMCPInventoryURLParams
	gotUsage     []telemetryrepo.ListShadowMCPInventoryUsageParams
}

func (f *fakeReader) GetShadowMCPInventoryURL(_ context.Context, arg telemetryrepo.GetShadowMCPInventoryURLParams) (*telemetryrepo.ShadowMCPInventoryURLRow, error) {
	f.gotInventory = append(f.gotInventory, arg)
	if f.rowErr != nil {
		return nil, f.rowErr
	}

	return f.row, nil
}

func (f *fakeReader) ListShadowMCPInventoryUsage(_ context.Context, arg telemetryrepo.ListShadowMCPInventoryUsageParams) ([]telemetryrepo.ShadowMCPInventoryUsageRow, error) {
	f.gotUsage = append(f.gotUsage, arg)
	if f.usageErr != nil {
		return nil, f.usageErr
	}

	return f.usage, nil
}

func inventoryRow(url string) *telemetryrepo.ShadowMCPInventoryURLRow {
	return &telemetryrepo.ShadowMCPInventoryURLRow{
		CanonicalServerURL: url,
		URLHost:            "mcp.example.com",
		ServerName:         "example",
		ServerNameOverride: "",
		FirstSeen:          time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		LastSeen:           time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		LastCalledUnixNano: 0,
		UpdatedAt:          time.Time{},
	}
}

func usageRow(url string, calls, users uint64) telemetryrepo.ShadowMCPInventoryUsageRow {
	first := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	last := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	return telemetryrepo.ShadowMCPInventoryUsageRow{
		CanonicalServerURL: url,
		ServerName:         "example",
		FirstCalled:        &first,
		LastCalled:         &last,
		CallCount:          calls,
		UserCount:          users,
		TopUsers:           nil,
	}
}

func TestAssess_SeenAndInUse(t *testing.T) {
	t.Parallel()

	const target = "https://mcp.example.com/sse"
	projectID := uuid.New()

	reader := &fakeReader{
		row: inventoryRow(target), usage: []telemetryrepo.ShadowMCPInventoryUsageRow{usageRow(target, 412, 14)},
		rowErr: nil, usageErr: nil, gotInventory: nil, gotUsage: nil,
	}

	got, err := exposure.Assess(t.Context(), reader, projectID, target)
	require.NoError(t, err)

	require.Equal(t, exposure.StatusSeen, got.Status)
	require.Equal(t, "example", got.ServerName)
	require.Equal(t, uint64(412), got.CallCount)

	// The number that decides what a denial costs.
	require.Equal(t, uint64(14), got.UserCount)
	require.True(t, got.InUse())
	require.Equal(t, 2026, got.FirstSeen.Year())
	require.False(t, got.FirstCalled.IsZero())

	// Every read is bounded by the project it was asked about.
	require.Len(t, reader.gotInventory, 1)
	require.Equal(t, projectID.String(), reader.gotInventory[0].GramProjectID)
	require.Len(t, reader.gotUsage, 1)
	require.Equal(t, projectID.String(), reader.gotUsage[0].GramProjectID)
}

// A server nobody here has touched is a real finding, and denying it costs
// nobody anything.
func TestAssess_UnseenIsAFinding(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{row: nil, usage: nil, rowErr: nil, usageErr: nil, gotInventory: nil, gotUsage: nil}

	got, err := exposure.Assess(t.Context(), reader, uuid.New(), "https://mcp.example.com/sse")
	require.NoError(t, err)

	require.Equal(t, exposure.StatusUnseen, got.Status)
	require.False(t, got.InUse())
	require.Zero(t, got.UserCount)

	// The canonical key is still reported, so a reviewer can see what was
	// searched for rather than trusting the request matched.
	require.NotEmpty(t, got.CanonicalURL)

	// Nothing here has recorded the server, so usage is not asked about.
	require.Empty(t, reader.gotUsage)
}

// "We could not look" must never read as "nobody uses this". An stdio launch
// command has no URL to key the inventory by.
func TestAssess_UnaddressableIsNotUnseen(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{row: nil, usage: nil, rowErr: nil, usageErr: nil, gotInventory: nil, gotUsage: nil}

	for _, target := range []string{"npx -y @scope/mcp-server", "", "   ", "not a url"} {
		got, err := exposure.Assess(t.Context(), reader, uuid.New(), target)
		require.NoError(t, err)
		require.Equal(t, exposure.StatusUnaddressable, got.Status, "target %q", target)
		require.NotEqual(t, exposure.StatusUnseen, got.Status)
		require.Empty(t, got.CanonicalURL)
		require.False(t, got.InUse())
	}

	// Nothing was asked of the store at all.
	require.Empty(t, reader.gotInventory)
	require.Empty(t, reader.gotUsage)
}

// The lookup key is the same canonical form the inventory is written with, so
// requests that differ only cosmetically still match.
func TestAssess_CanonicalizesTheLookupKey(t *testing.T) {
	t.Parallel()

	reader := &fakeReader{row: nil, usage: nil, rowErr: nil, usageErr: nil, gotInventory: nil, gotUsage: nil}

	_, err := exposure.Assess(t.Context(), reader, uuid.New(), "HTTPS://MCP.Example.com:443/sse?token=abc#frag")
	require.NoError(t, err)
	require.Len(t, reader.gotInventory, 1)

	key := reader.gotInventory[0].CanonicalServerURL
	require.NotContains(t, key, "token=abc", "a credential in the query string must not become the lookup key")
	require.NotContains(t, key, "#frag")
	require.NotContains(t, key, "MCP.Example.com")
}

// A server discovered but never called is distinguishable from one in use.
func TestAssess_InInventoryButNeverCalled(t *testing.T) {
	t.Parallel()

	const target = "https://mcp.example.com/sse"
	reader := &fakeReader{
		row: inventoryRow(target), usage: nil,
		rowErr: nil, usageErr: nil, gotInventory: nil, gotUsage: nil,
	}

	got, err := exposure.Assess(t.Context(), reader, uuid.New(), target)
	require.NoError(t, err)

	require.Equal(t, exposure.StatusSeen, got.Status)
	require.False(t, got.InUse())
	require.Zero(t, got.CallCount)
	require.True(t, got.FirstCalled.IsZero())
}

// Usage rows are matched on their key, so a widened result set can never
// attribute another server's traffic to this one.
func TestAssess_IgnoresUsageForAnotherServer(t *testing.T) {
	t.Parallel()

	const target = "https://mcp.example.com/sse"
	reader := &fakeReader{
		row: inventoryRow(target),
		usage: []telemetryrepo.ShadowMCPInventoryUsageRow{
			usageRow("https://other.example.com/sse", 9999, 500),
			usageRow(target, 3, 1),
		},
		rowErr: nil, usageErr: nil, gotInventory: nil, gotUsage: nil,
	}

	got, err := exposure.Assess(t.Context(), reader, uuid.New(), target)
	require.NoError(t, err)
	require.Equal(t, uint64(3), got.CallCount)
	require.Equal(t, uint64(1), got.UserCount)
}

// An admin-set name wins over the one observed in traffic.
func TestAssess_PrefersNameOverride(t *testing.T) {
	t.Parallel()

	const target = "https://mcp.example.com/sse"
	row := inventoryRow(target)
	row.ServerNameOverride = "Vendor (approved name)"

	reader := &fakeReader{
		row: row, usage: nil, rowErr: nil, usageErr: nil, gotInventory: nil, gotUsage: nil,
	}

	got, err := exposure.Assess(t.Context(), reader, uuid.New(), target)
	require.NoError(t, err)
	require.Equal(t, "Vendor (approved name)", got.ServerName)
}

// A failed lookup is an error, never a quietly empty result that would read as
// "nobody uses this".
func TestAssess_ReadFailuresAreErrors(t *testing.T) {
	t.Parallel()

	const target = "https://mcp.example.com/sse"

	inventoryFailure := &fakeReader{
		row: nil, usage: nil, rowErr: errors.New("clickhouse down"), usageErr: nil,
		gotInventory: nil, gotUsage: nil,
	}
	_, err := exposure.Assess(t.Context(), inventoryFailure, uuid.New(), target)
	require.Error(t, err)

	usageFailure := &fakeReader{
		row: inventoryRow(target), usage: nil, rowErr: nil, usageErr: errors.New("clickhouse down"),
		gotInventory: nil, gotUsage: nil,
	}
	_, err = exposure.Assess(t.Context(), usageFailure, uuid.New(), target)
	require.Error(t, err)
}
