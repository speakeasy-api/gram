package mv

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func clientRow() repo.UserSessionClient {
	return repo.UserSessionClient{
		ID:                             uuid.New(),
		ProjectID:                      uuid.NullUUID{UUID: uuid.New(), Valid: true},
		UserSessionIssuerID:            uuid.New(),
		ClientID:                       "gram_client_abc",
		ClientSecretHash:               pgtype.Text{String: "", Valid: false},
		ClientName:                     "Some Agent",
		RedirectUris:                   []string{"http://127.0.0.1:1234/callback"},
		ClientIDIssuedAt:               ts(time.Now()),
		ClientSecretExpiresAt:          pgtype.Timestamptz{Time: time.Time{}, Valid: false},
		ClientIDMetadataUri:            pgtype.Text{String: "", Valid: false},
		ClientIDMetadataFetchedAt:      pgtype.Timestamptz{Time: time.Time{}, Valid: false},
		ClientIDMetadataCacheExpiresAt: pgtype.Timestamptz{Time: time.Time{}, Valid: false},
		ClientIDMetadataEtag:           pgtype.Text{String: "", Valid: false},
		TokenEndpointAuthMethod:        pgtype.Text{String: "", Valid: false},
		ClientJwks:                     nil,
		ClientJwksUri:                  pgtype.Text{String: "", Valid: false},
		CreatedAt:                      ts(time.Now()),
		UpdatedAt:                      ts(time.Now()),
		DeletedAt:                      pgtype.Timestamptz{Time: time.Time{}, Valid: false},
		Deleted:                        false,
	}
}

func TestBuildUserSessionClientView_ReportsDeclaredMethodAndKind(t *testing.T) {
	t.Parallel()

	row := clientRow()
	row.TokenEndpointAuthMethod = pgtype.Text{String: "private_key_jwt", Valid: true}

	got := BuildUserSessionClientView(row, 3)

	require.Equal(t, "key", got.CredentialKind)
	require.NotNil(t, got.TokenEndpointAuthMethod)
	require.Equal(t, "private_key_jwt", *got.TokenEndpointAuthMethod)
	require.Equal(t, 3, got.ActiveSessionCount)
}

// A row that predates the column resolves off the secret it stores rather than
// reading as unknown, matching what the token endpoint holds it to.
func TestBuildUserSessionClientView_LegacyRowResolvesOffStoredSecret(t *testing.T) {
	t.Parallel()

	withSecret := clientRow()
	withSecret.ClientSecretHash = pgtype.Text{String: "$2a$10$hash", Valid: true}

	got := BuildUserSessionClientView(withSecret, 0)

	require.Equal(t, "secret", got.CredentialKind)
	require.Nil(t, got.TokenEndpointAuthMethod, "a row that declared nothing must not report a declared method")

	got = BuildUserSessionClientView(clientRow(), 0)

	require.Equal(t, "public", got.CredentialKind)
	require.Nil(t, got.TokenEndpointAuthMethod)
}

// A registration whose columns contradict each other cannot authenticate at
// all. The view names that state rather than repeating the declared method,
// which would read as a working client.
func TestBuildUserSessionClientView_ContradictoryRowIsMisconfigured(t *testing.T) {
	t.Parallel()

	row := clientRow()
	row.TokenEndpointAuthMethod = pgtype.Text{String: "private_key_jwt", Valid: true}
	row.ClientSecretHash = pgtype.Text{String: "$2a$10$hash", Valid: true}

	got := BuildUserSessionClientView(row, 0)

	require.Equal(t, "misconfigured", got.CredentialKind)
	require.NotNil(t, got.TokenEndpointAuthMethod)
	require.Equal(t, "private_key_jwt", *got.TokenEndpointAuthMethod, "the declared value stays visible so the contradiction can be diagnosed")
}
