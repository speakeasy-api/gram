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

// Half a connection is an incomplete identity, not an absent one. Taking the
// connection-less path for it would record a user-attributed write against a
// connection the caller never proved, and would violate the database's own
// pair constraint.
func TestPrincipalConnectionRejectsAHalfPopulatedPair(t *testing.T) {
	t.Parallel()

	full := Principal{UserID: "user", OrganizationID: "org", ConnectionID: uuid.NewString(), Generation: uuid.NewString()}
	connectionID, generation, err := principalConnection(full)
	require.NoError(t, err)
	require.True(t, connectionID.Valid)
	require.True(t, generation.Valid)

	none := Principal{UserID: "user", OrganizationID: "org"}
	connectionID, generation, err = principalConnection(none)
	require.NoError(t, err, "a surface with no connection is legitimate")
	require.False(t, connectionID.Valid)
	require.False(t, generation.Valid)

	for _, incomplete := range []Principal{
		{UserID: "user", OrganizationID: "org", ConnectionID: uuid.NewString()},
		{UserID: "user", OrganizationID: "org", Generation: uuid.NewString()},
	} {
		require.True(t, incomplete.HasConnection(), "either half is a claim to a connection")
		_, _, err := principalConnection(incomplete)
		require.Error(t, err, "an incomplete connection identity must not be treated as connection-less")
	}
}

// Budget metering and cursor binding must classify a caller the same way. When
// they disagreed, a half-populated identity was metered as a connected caller
// but paginated as a connection-less one — the same principal counted as two
// different callers depending on which helper looked at it.
func TestCallerClassificationIsConsistentAcrossHelpers(t *testing.T) {
	t.Parallel()

	connectionID, generation := uuid.NewString(), uuid.NewString()

	t.Run("a full pair is metered and bound by its connection", func(t *testing.T) {
		t.Parallel()

		principal := Principal{UserID: "user", OrganizationID: "org", ConnectionID: connectionID, Generation: generation}
		require.True(t, principal.HasConnection())
		require.Equal(t, connectionID, callerBudgetKey(principal))
		require.Equal(t, generation, principalCursorBinding(principal))
	})

	t.Run("no connection is metered and bound by its subject", func(t *testing.T) {
		t.Parallel()

		principal := Principal{UserID: "user", OrganizationID: "org", Surface: SurfaceProjectAssistant}
		require.False(t, principal.HasConnection())
		require.Equal(t, userSubjectURN("user"), callerBudgetKey(principal))
		require.Equal(t, string(SurfaceProjectAssistant)+":"+userSubjectURN("user"), principalCursorBinding(principal))
	})

	// A half-populated pair is a connection claim the caller cannot prove. Both
	// helpers take the connected branch and find nothing there, so the budget
	// reports unavailable and the cursor cannot be bound — failing closed in
	// the same direction rather than metering one way and paginating another.
	for _, principal := range []Principal{
		{UserID: "user", OrganizationID: "org", ConnectionID: connectionID},
		{UserID: "user", OrganizationID: "org", Generation: generation},
	} {
		require.True(t, principal.HasConnection(), "either half is a claim to a connection")
		budgetKey, cursorBinding := callerBudgetKey(principal), principalCursorBinding(principal)
		require.True(t, budgetKey == "" || cursorBinding == "",
			"a half pair must fail closed rather than be metered or bound as a proven caller")
	}
}
