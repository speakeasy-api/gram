package otel

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
)

func TestEnrichLogDirectoryIncludesCachedUserEnrichment(t *testing.T) {
	t.Parallel()

	const organizationID = "organization-id"
	const email = "user@example.invalid"
	emailDigest := sha256.Sum256([]byte(email))
	emailHash := hex.EncodeToString(emailDigest[:])

	enricher := newEnrichLogDirectory(testenv.NewLogger(t), nil, testenv.NewMemoryCache())
	require.NoError(t, enricher.cache.Store(t.Context(), cachedUserEnrichment{
		OrganizationID: organizationID,
		EmailHash:      emailHash,
		Enrichment: userEnrichment{
			DirectoryID:         "directory-user-id",
			DirectoryAttributes: map[string]any{"department": "Engineering"},
			DirectoryGroupIDs:   []string{"directory-group-id"},
			DirectoryGroupNames: []string{"Developers"},
			Roles:               []string{"member"},
		},
	}))

	record := (&otelv1.InboundLogRecord_builder{
		Provenance: (&otelv1.InboundLogRecord_Provenance_builder{OrganizationId: new(organizationID)}).Build(),
		Attributes: []*otelv1.InboundLogRecord_KeyValue{logStringAttribute("user.email", " User@Example.Invalid ")},
	}).Build()
	got, err := enricher.Enrich(t.Context(), record)

	require.NoError(t, err)
	require.ElementsMatch(t, []attribute.KeyValue{
		DirectoryID("directory-user-id"),
		DirectoryAttribute("department").String("Engineering"),
		DirectoryGroupIDs([]string{"directory-group-id"}),
		DirectoryGroupNames([]string{"Developers"}),
		GramUserRoles([]string{"member"}),
	}, got)
}

func TestEnrichLogDirectoryResolvesSubaddress(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	seed := seedUserEnrichment(t, db)
	enricher := newEnrichLogDirectory(testenv.NewLogger(t), db, cache.NoopCache)
	record := (&otelv1.InboundLogRecord_builder{
		Provenance: (&otelv1.InboundLogRecord_Provenance_builder{OrganizationId: new(seed.organizationID)}).Build(),
		Attributes: []*otelv1.InboundLogRecord_KeyValue{logStringAttribute("user.email", "user+log@example.invalid")},
	}).Build()

	got, err := enricher.Enrich(t.Context(), record)

	require.NoError(t, err)
	require.ElementsMatch(t, seed.want.attributes(), got)
}

func TestEnrichLogDirectoryPrefersRawSubaddress(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	canonical := seedUserEnrichment(t, db)
	raw := seedSubaddressUserEnrichment(t, db, canonical.organizationID)
	enricher := newEnrichLogDirectory(testenv.NewLogger(t), db, cache.NoopCache)
	record := (&otelv1.InboundLogRecord_builder{
		Provenance: (&otelv1.InboundLogRecord_Provenance_builder{OrganizationId: new(raw.organizationID)}).Build(),
		Attributes: []*otelv1.InboundLogRecord_KeyValue{logStringAttribute("user.email", raw.email)},
	}).Build()

	got, err := enricher.Enrich(t.Context(), record)

	require.NoError(t, err)
	require.ElementsMatch(t, raw.want.attributes(), got)
}
