package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/stretchr/testify/require"
)

func TestCatalogRegistrationInputHashIsStableAndTargetBound(t *testing.T) {
	t.Parallel()

	baseline := catalogRegistrationInputHash("payments", "catalog", "registry", "acme/linear")
	require.Equal(t, baseline, catalogRegistrationInputHash("payments", "catalog", "registry", "acme/linear"))
	require.NotEqual(t, baseline, catalogRegistrationInputHash("other-project", "catalog", "registry", "acme/linear"))
	require.NotEqual(t, baseline, catalogRegistrationInputHash("payments", "catalog", "registry", "other/reference"))
}

func TestValidateCatalogRegistrationRequest(t *testing.T) {
	t.Parallel()

	project := ResolvedProject{ID: uuid.New(), Slug: "project"}
	principal := Principal{UserID: "user", OrganizationID: "organization"}
	request := CatalogRegistrationRequest{
		ProjectSlug:      project.Slug,
		SourceKind:       "catalog",
		CatalogProvider:  "registry",
		CatalogReference: "acme/linear",
		IdempotencyKey:   "key",
		InputHash:        catalogRegistrationInputHash(project.Slug, "catalog", "registry", "acme/linear"),
	}

	require.NoError(t, validateCatalogRegistrationRequest(principal, project, request))

	t.Run("rejects an input hash that does not match normalized identity", func(t *testing.T) {
		t.Parallel()

		invalid := request
		invalid.InputHash = catalogRegistrationInputHash(project.Slug, invalid.SourceKind, invalid.CatalogProvider, "other/reference")
		require.ErrorIs(t, validateCatalogRegistrationRequest(principal, project, invalid), ErrRegistrationInvalid)
	})

	t.Run("rejects a project mismatch", func(t *testing.T) {
		t.Parallel()

		invalid := request
		invalid.ProjectSlug = "other-project"
		require.ErrorIs(t, validateCatalogRegistrationRequest(principal, project, invalid), ErrRegistrationInvalid)
	})

	t.Run("rejects an overlong idempotency key", func(t *testing.T) {
		t.Parallel()

		invalid := request
		invalid.IdempotencyKey = string(make([]byte, 129))
		require.ErrorIs(t, validateCatalogRegistrationRequest(principal, project, invalid), ErrRegistrationInvalid)
	})
}

func TestOperationReceiptFromRowPreservesRegistrationAssociation(t *testing.T) {
	t.Parallel()

	registrationID := uuid.New()
	receipt := operationReceiptFromRow(platformrepo.PlatformMcpOperationReceipt{
		ID:             uuid.New(),
		RegistrationID: uuid.NullUUID{UUID: registrationID, Valid: true},
		Status:         receiptStatusPending,
	}, false)

	require.Equal(t, registrationID, receipt.RegistrationID.UUID)
	require.True(t, receipt.RegistrationID.Valid)
	require.Equal(t, receiptStatusPending, receipt.Status)
}

func TestNewRegistrationStoreRequiresDatabase(t *testing.T) {
	t.Parallel()

	store, err := NewRegistrationStore(nil, RegistrationStoreConfig{ActiveRegistrationCap: 1})
	require.ErrorIs(t, err, ErrRegistrationInvalid)
	require.Nil(t, store)
}

func TestPlatformMCPEndpointSlugStaysWithinDatabaseCharacterLimit(t *testing.T) {
	t.Parallel()

	slug := platformMCPEndpointSlug("🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀", "0123456789abcdef")

	require.Len(t, []rune(slug), maxMCPEndpointSlugLength)
	require.Contains(t, slug, "-platform-mcp-endpoint-0123456789abcdef")
}

func TestValidRegistrationRemoteURL(t *testing.T) {
	t.Parallel()

	require.True(t, validRegistrationRemoteURL("https://provider.test/mcp?tenant=reviewed"))
	require.False(t, validRegistrationRemoteURL("http://provider.test/mcp"))
	require.False(t, validRegistrationRemoteURL("https://user:password@provider.test/mcp"))
	require.False(t, validRegistrationRemoteURL("https://provider.test/mcp#fragment"))
	require.False(t, validRegistrationRemoteURL("https://provider.test/{tenant}/mcp"))
}

func TestNewRegistrationComponentSuffixIsCollisionResistant(t *testing.T) {
	t.Parallel()

	first, err := newRegistrationComponentSuffix()
	require.NoError(t, err)
	second, err := newRegistrationComponentSuffix()
	require.NoError(t, err)

	require.Len(t, first, 16)
	require.NotEqual(t, first, second)
}

func TestUserSubjectURN(t *testing.T) {
	t.Parallel()

	require.Equal(t, "user:user-id", userSubjectURN("user-id"))
}
