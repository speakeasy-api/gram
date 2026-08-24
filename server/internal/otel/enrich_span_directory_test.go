package otel

import (
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestEnrichDirectoryReturnsNoAttributesWithoutMatchingUser(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	enricher := NewEnrichDirectory(testenv.NewLogger(t), db, cache.NoopCache)

	got, err := enricher.Enrich(t.Context(), directoryEnrichmentTestSpan("organization-id", " User@Example.Invalid "))

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestEnrichDirectoryResolvesSubaddress(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	seed := seedUserEnrichment(t, db)
	enricher := NewEnrichDirectory(testenv.NewLogger(t), db, cache.NoopCache)

	got, err := enricher.Enrich(t.Context(), directoryEnrichmentTestSpan(seed.organizationID, "user+span@example.invalid"))

	require.NoError(t, err)
	require.ElementsMatch(t, seed.want.attributes(), got)
}

func TestEnrichDirectoryIncludesDirectoryAndRoleAttributes(t *testing.T) {
	t.Parallel()

	db := newTestDatabase(t)
	seed := seedUserEnrichment(t, db)
	enricher := NewEnrichDirectory(testenv.NewLogger(t), db, cache.NoopCache)

	got, err := enricher.Enrich(t.Context(), directoryEnrichmentTestSpan(seed.organizationID, seed.email))

	require.NoError(t, err)
	require.ElementsMatch(t, seed.want.attributes(), got)
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
	return (&otelv1.InboundSpan_builder{
		Provenance: (&otelv1.InboundSpan_Provenance_builder{OrganizationId: &organizationID}).Build(),
		Attributes: []*otelv1.InboundSpan_KeyValue{
			spanStringAttribute("user.email", email),
		},
	}).Build()
}
