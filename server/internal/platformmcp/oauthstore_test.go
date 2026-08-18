package platformmcp

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

func TestConnectionFromRowDerivesLegacyAuthorizationDeadline(t *testing.T) {
	t.Parallel()

	authorizedAt := time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
	reauthorizedAt := authorizedAt.Add(24 * time.Hour)
	row := platformrepo.PlatformMcpConnection{
		ID:                     uuid.New(),
		OrganizationID:         "organization-1",
		SubjectUrn:             "user:user-1",
		ActiveGeneration:       uuid.New(),
		AuthorizedAt:           pgtype.Timestamptz{Time: authorizedAt, Valid: true},
		ReauthorizedAt:         pgtype.Timestamptz{Time: reauthorizedAt, Valid: true},
		AuthorizationExpiresAt: pgtype.Timestamptz{},
	}

	connection := connectionFromRow(row, "client-1")
	require.Equal(t, reauthorizedAt.Add(platformoauth.AuthorizationLifetime), connection.AuthorizationExpiresAt)
}

func TestVerifyPKCE(t *testing.T) {
	t.Parallel()

	verifier := strings.Repeat("a", 43)
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	require.NoError(t, verifyPKCE(verifier, challenge))
	weakHash := sha256.Sum256([]byte("a"))
	require.Error(t, verifyPKCE("a", base64.RawURLEncoding.EncodeToString(weakHash[:])))
	require.Error(t, verifyPKCE(strings.Repeat("a", 43), strings.Repeat("!", 43)))
}

func TestValidPKCES256Challenge(t *testing.T) {
	t.Parallel()

	require.True(t, validPKCES256Challenge(strings.Repeat("a", 43)))
	require.False(t, validPKCES256Challenge("short"))
	require.False(t, validPKCES256Challenge(strings.Repeat("!", 43)))
}
