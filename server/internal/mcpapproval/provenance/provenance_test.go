package provenance_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/provenance"
)

// liveMeta is the shape a real catalogue entry carries, copied from an
// authenticated registry response rather than invented.
const liveMeta = `{
  "com.pulsemcp/server": {
    "isOfficial": true,
    "visitorsEstimateMostRecentWeek": 120,
    "visitorsEstimateLastFourWeeks": 500,
    "visitorsEstimateTotal": 9000
  },
  "com.pulsemcp/server-version": {
    "source": "pulsemcp.com",
    "status": "active",
    "publishedAt": "2026-04-14T22:19:00Z",
    "updatedAt": "2026-04-16T00:09:24Z",
    "isLatest": true
  }
}`

func decode(t *testing.T, raw string) any {
	t.Helper()

	var value any
	require.NoError(t, json.Unmarshal([]byte(raw), &value))

	return value
}

func TestRead_LiveShape(t *testing.T) {
	t.Parallel()

	got := provenance.Read(decode(t, liveMeta))

	require.True(t, got.Catalogued)
	require.True(t, got.Official)
	require.Equal(t, "active", got.Status)
	require.True(t, got.IsLatest)
	require.Equal(t, 2026, got.PublishedAt.Year())
	require.Equal(t, 9000, got.VisitorsTotal)
	require.Equal(t, 500, got.VisitorsLastFourWeeks)
	require.Equal(t, 120, got.VisitorsLastWeek)
	require.False(t, got.Withdrawn())
}

// A server the registry has never heard of is the common case for this
// workflow. It must be distinguishable from a catalogued server with sparse
// metadata, so the surface can say "not catalogued" rather than showing blanks.
func TestRead_AbsentMetaIsNotCatalogued(t *testing.T) {
	t.Parallel()

	got := provenance.Read(nil)

	require.False(t, got.Catalogued)
	require.False(t, got.Official)
	require.Zero(t, got.VisitorsTotal)
}

// An unreadable blob tells us no more than an absent one.
func TestRead_UnparseableMetaIsNotCatalogued(t *testing.T) {
	t.Parallel()

	require.False(t, provenance.Read("not an object").Catalogued)
	require.False(t, provenance.Read(decode(t, `[]`)).Catalogued)
}

// A catalogued entry that publishes nothing useful is still catalogued: the
// registry knows the server, it just says little about it.
func TestRead_EmptyObjectIsCataloguedButBare(t *testing.T) {
	t.Parallel()

	got := provenance.Read(decode(t, `{}`))

	require.True(t, got.Catalogued)
	require.False(t, got.Official)
	require.Empty(t, got.Status)
	require.True(t, got.PublishedAt.IsZero())
}

// A cache hit rebuilds the blob as plain maps rather than the original struct,
// so reading must not depend on the concrete type it arrived as.
func TestRead_WorksOnRebuiltMaps(t *testing.T) {
	t.Parallel()

	rebuilt := map[string]any{
		"com.pulsemcp/server": map[string]any{
			"isOfficial":            true,
			"visitorsEstimateTotal": float64(42),
		},
		"com.pulsemcp/server-version": map[string]any{
			"status":    "active",
			"isLatest":  true,
			"updatedAt": "2026-01-02T03:04:05Z",
		},
	}

	got := provenance.Read(rebuilt)

	require.True(t, got.Catalogued)
	require.True(t, got.Official)
	require.Equal(t, 42, got.VisitorsTotal)
	require.Equal(t, 2026, got.UpdatedAt.Year())
}

// A server withdrawn from the catalogue after someone started using it is the
// one status change that moves a decision on its own.
func TestProvenance_Withdrawn(t *testing.T) {
	t.Parallel()

	deleted := provenance.Read(decode(t, `{"com.pulsemcp/server-version":{"status":"deleted"}}`))
	require.True(t, deleted.Withdrawn())

	active := provenance.Read(decode(t, `{"com.pulsemcp/server-version":{"status":"active"}}`))
	require.False(t, active.Withdrawn())

	// Not catalogued at all is not the same as withdrawn.
	require.False(t, provenance.Read(nil).Withdrawn())

	// A catalogued entry with no status published is not evidence of removal.
	require.False(t, provenance.Read(decode(t, `{}`)).Withdrawn())
}

func TestProvenance_StaleFor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)

	got := provenance.Read(decode(t, `{"com.pulsemcp/server-version":{"updatedAt":"2026-08-01T00:00:00Z"}}`))
	age, known := got.StaleFor(now)
	require.True(t, known)
	require.Equal(t, 6*24*time.Hour, age)

	// Unknown must be distinguishable from zero age.
	_, known = provenance.Read(decode(t, `{}`)).StaleFor(now)
	require.False(t, known)
}
