package mv

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func ts(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func TestBuildUserSessionView_ResolvesUser(t *testing.T) {
	t.Parallel()

	row := repo.ListUserSessionsByProjectIDRow{
		ID:                  uuid.New(),
		UserSessionIssuerID: uuid.New(),
		SubjectUrn:          urn.NewUserSubject("user-123"),
		Jti:                 "jti-1",
		RefreshExpiresAt:    ts(time.Now()),
		ExpiresAt:           ts(time.Now()),
		CreatedAt:           ts(time.Now()),
		UpdatedAt:           ts(time.Now()),
		IssuerSlug:          "my-issuer",
		ClientName:          pgtype.Text{String: "Claude Desktop", Valid: true},
		UserDisplayName:     pgtype.Text{String: "Ada Lovelace", Valid: true},
		UserEmail:           pgtype.Text{String: "ada@example.com", Valid: true},
		Deleted:             false,
	}

	got := BuildUserSessionView(row, nil)

	require.Equal(t, "my-issuer", got.IssuerSlug)
	require.Equal(t, "user", got.SubjectType)
	require.NotNil(t, got.ClientName)
	require.Equal(t, "Claude Desktop", *got.ClientName)
	require.NotNil(t, got.SubjectDisplayName)
	require.Equal(t, "Ada Lovelace", *got.SubjectDisplayName)
	require.Nil(t, got.RevokedAt)
}

func TestBuildUserSessionView_UserFallsBackToEmail(t *testing.T) {
	t.Parallel()

	row := repo.ListUserSessionsByProjectIDRow{
		ID:               uuid.New(),
		SubjectUrn:       urn.NewUserSubject("user-123"),
		RefreshExpiresAt: ts(time.Now()), ExpiresAt: ts(time.Now()),
		CreatedAt: ts(time.Now()), UpdatedAt: ts(time.Now()),
		IssuerSlug:      "iss",
		UserDisplayName: pgtype.Text{Valid: false},
		UserEmail:       pgtype.Text{String: "ada@example.com", Valid: true},
	}

	got := BuildUserSessionView(row, nil)
	require.NotNil(t, got.SubjectDisplayName)
	require.Equal(t, "ada@example.com", *got.SubjectDisplayName)
}

func TestBuildUserSessionView_APIKeyAndRevoked(t *testing.T) {
	t.Parallel()

	revokedAt := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	row := repo.ListUserSessionsByProjectIDRow{
		ID:               uuid.New(),
		SubjectUrn:       urn.NewAPIKeySubject(uuid.New()),
		RefreshExpiresAt: ts(time.Now()), ExpiresAt: ts(time.Now()),
		CreatedAt: ts(time.Now()), UpdatedAt: ts(time.Now()),
		IssuerSlug: "iss",
		ApiKeyName: pgtype.Text{String: "ci-key", Valid: true},
		DeletedAt:  ts(revokedAt),
		Deleted:    true,
	}

	got := BuildUserSessionView(row, nil)
	require.Equal(t, "apikey", got.SubjectType)
	require.NotNil(t, got.SubjectDisplayName)
	require.Equal(t, "ci-key", *got.SubjectDisplayName)
	require.NotNil(t, got.RevokedAt)
	require.Equal(t, revokedAt.Format(time.RFC3339), *got.RevokedAt)
}

func TestBuildUserSessionView_AnonymousHasNoName(t *testing.T) {
	t.Parallel()

	row := repo.ListUserSessionsByProjectIDRow{
		ID:               uuid.New(),
		SubjectUrn:       urn.NewAnonymousSubject("mcp-sess-1"),
		RefreshExpiresAt: ts(time.Now()), ExpiresAt: ts(time.Now()),
		CreatedAt: ts(time.Now()), UpdatedAt: ts(time.Now()),
		IssuerSlug: "iss",
	}

	got := BuildUserSessionView(row, nil)
	require.Equal(t, "anonymous", got.SubjectType)
	require.Nil(t, got.SubjectDisplayName)
}

func TestBuildUserSessionView_ResolvesClientCredentialKind(t *testing.T) {
	t.Parallel()

	row := repo.ListUserSessionsByProjectIDRow{
		ID:                            uuid.New(),
		UserSessionIssuerID:           uuid.New(),
		UserSessionClientID:           uuid.NullUUID{UUID: uuid.New(), Valid: true},
		SubjectUrn:                    urn.NewUserSubject("user-123"),
		Jti:                           "jti-1",
		RefreshExpiresAt:              ts(time.Now()),
		ExpiresAt:                     ts(time.Now()),
		CreatedAt:                     ts(time.Now()),
		UpdatedAt:                     ts(time.Now()),
		IssuerSlug:                    "my-issuer",
		ClientTokenEndpointAuthMethod: pgtype.Text{String: "private_key_jwt", Valid: true},
		ClientHasSecret:               false,
	}

	got := BuildUserSessionView(row, nil)

	require.NotNil(t, got.ClientCredentialKind)
	require.Equal(t, "key", *got.ClientCredentialKind)
	require.NotNil(t, got.ClientTokenEndpointAuthMethod)
	require.Equal(t, "private_key_jwt", *got.ClientTokenEndpointAuthMethod)
}

// A client that predates the token_endpoint_auth_method column still resolves
// to a kind, off the secret it stores. Reporting it as unknown would tell an
// operator less than the token endpoint already knows.
func TestBuildUserSessionView_LegacyClientResolvesOffStoredSecret(t *testing.T) {
	t.Parallel()

	row := repo.ListUserSessionsByProjectIDRow{
		ID:                  uuid.New(),
		UserSessionIssuerID: uuid.New(),
		UserSessionClientID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
		SubjectUrn:          urn.NewUserSubject("user-123"),
		Jti:                 "jti-1",
		RefreshExpiresAt:    ts(time.Now()),
		ExpiresAt:           ts(time.Now()),
		CreatedAt:           ts(time.Now()),
		UpdatedAt:           ts(time.Now()),
		IssuerSlug:          "my-issuer",
		ClientHasSecret:     true,
	}

	got := BuildUserSessionView(row, nil)

	require.NotNil(t, got.ClientCredentialKind)
	require.Equal(t, "secret", *got.ClientCredentialKind)
	require.Nil(t, got.ClientTokenEndpointAuthMethod, "a row that declared nothing must not report a declared method")
}

// The join that lifts the client columns is a LEFT JOIN, so an unbound session
// reads exactly like a legacy client row. Neither field may be reported.
func TestBuildUserSessionView_NoBoundClientReportsNoCredentialFields(t *testing.T) {
	t.Parallel()

	row := repo.ListUserSessionsByProjectIDRow{
		ID:                  uuid.New(),
		UserSessionIssuerID: uuid.New(),
		SubjectUrn:          urn.NewAPIKeySubject(uuid.New()),
		Jti:                 "jti-1",
		RefreshExpiresAt:    ts(time.Now()),
		ExpiresAt:           ts(time.Now()),
		CreatedAt:           ts(time.Now()),
		UpdatedAt:           ts(time.Now()),
		IssuerSlug:          "my-issuer",
	}

	got := BuildUserSessionView(row, nil)

	require.Nil(t, got.ClientCredentialKind)
	require.Nil(t, got.ClientTokenEndpointAuthMethod)
}
