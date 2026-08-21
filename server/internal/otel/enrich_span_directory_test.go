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

	logger := testenv.NewLogger(t)
	db := newTestDatabase(t)
	cacheImpl := testenv.NewMemoryCache()
	directoryEnricher := NewEnrichDirectory(logger, db, cacheImpl)
	require.NoError(t, directoryEnricher.cache.Store(t.Context(), cachedUserEnrichment{
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
	got, err := newEnrichLogDirectory(logger, db, cacheImpl).Enrich(t.Context(), record)

	require.NoError(t, err)
	require.ElementsMatch(t, []attribute.KeyValue{
		DirectoryID("directory-user-id"),
		DirectoryAttribute("department").String("Engineering"),
		DirectoryGroupIDs([]string{"directory-group-id"}),
		DirectoryGroupNames([]string{"Developers"}),
		GramUserRoles([]string{"member"}),
	}, got)
}

func TestEnrichDirectoryReturnsNoAttributesWithoutMatchingUser(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	enricher := NewEnrichDirectory(testenv.NewLogger(t), db, cache.NoopCache)

	got, err := enricher.Enrich(t.Context(), directoryEnrichmentTestSpan("organization-id", " User@Example.Invalid "))

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestEnrichDirectoryLookupFailureDoesNotFailSpan(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	db.Close()
	enricher := NewEnrichDirectory(testenv.NewLogger(t), db, cache.NoopCache)

	got, err := enricher.Enrich(t.Context(), directoryEnrichmentTestSpan("organization-id", "user@example.invalid"))

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestEnrichDirectoryRequiresTrustedOrganizationScope(t *testing.T) {
	t.Parallel()

	enricher := NewEnrichDirectory(testenv.NewLogger(t), newTestDatabase(t), cache.NoopCache)

	got, err := enricher.Enrich(t.Context(), directoryEnrichmentTestSpan("", "user@example.invalid"))

	require.NoError(t, err)
	require.Empty(t, got)
}
func directoryEnrichmentTestSpan(organizationID string, email string) *otelv1.InboundSpan {
	key := "user.email"
	return (&otelv1.InboundSpan_builder{
		Provenance: (&otelv1.InboundSpan_Provenance_builder{OrganizationId: &organizationID}).Build(),
		Attributes: []*otelv1.InboundSpan_KeyValue{
			(&otelv1.InboundSpan_KeyValue_builder{
				Key:   &key,
				Value: (&otelv1.InboundSpan_AnyValue_builder{StringValue: &email}).Build(),
			}).Build(),
		},
	}).Build()
}
