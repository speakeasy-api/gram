package otel

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
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
