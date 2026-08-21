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

func TestEnrichDirectoryIncludesProfileGroupsAndRoles(t *testing.T) {
	t.Parallel()

	userContext := directoryUserContext{
		DirectoryUserID: "directory-user-id",
		UserAttributes: map[string]any{
			"active":     true,
			"department": nil,
			"mixed":      []any{"one", float64(2), true},
			"nested":     map[string]any{"level": float64(7)},
			"skills":     []any{"Go", "SQL"},
		},
		GroupIDs:   []string{"directory-group-id"},
		GroupNames: []string{"Developers"},
		Roles:      []string{"member", "tool-author"},
	}

	require.ElementsMatch(t, []attribute.KeyValue{
		DirectoryID("directory-user-id"),
		DirectoryAttribute("active").Bool(true),
		DirectoryAttribute("mixed").Slice(
			attribute.StringValue("one"),
			attribute.Float64Value(2),
			attribute.BoolValue(true),
		),
		DirectoryAttribute("nested").String(`{"level":7}`),
		DirectoryAttribute("skills").Slice(attribute.StringValue("Go"), attribute.StringValue("SQL")),
		DirectoryGroupIDs([]string{"directory-group-id"}),
		DirectoryGroupNames([]string{"Developers"}),
		GramUserRoles([]string{"member", "tool-author"}),
	}, userContext.attributes())
	require.Equal(t, "directory.attribute.active", string(DirectoryAttribute("active")))
}

func TestEnrichLogDirectoryIncludesCachedUserContext(t *testing.T) {
	t.Parallel()

	const organizationID = "organization-id"
	const email = "user@example.invalid"
	emailDigest := sha256.Sum256([]byte(email))
	emailHash := hex.EncodeToString(emailDigest[:])

	directoryEnricher := NewEnrichDirectory(testenv.NewLogger(t), newTestDatabase(t), testenv.NewMemoryCache())
	require.NoError(t, directoryEnricher.cache.Store(t.Context(), cachedDirectoryUserContext{
		OrganizationID: organizationID,
		EmailHash:      emailHash,
		Context: directoryUserContext{
			DirectoryUserID: "directory-user-id",
			UserAttributes:  map[string]any{"department": "Engineering"},
			GroupIDs:        []string{"directory-group-id"},
			GroupNames:      []string{"Developers"},
			Roles:           []string{"member"},
		},
	}))

	record := (&otelv1.InboundLogRecord_builder{
		Provenance: (&otelv1.InboundLogRecord_Provenance_builder{OrganizationId: new(organizationID)}).Build(),
		Attributes: []*otelv1.InboundLogRecord_KeyValue{logStringAttribute("user.email", " User@Example.Invalid ")},
	}).Build()
	got, err := (&enrichLogDirectory{directory: directoryEnricher}).Enrich(t.Context(), record)

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
