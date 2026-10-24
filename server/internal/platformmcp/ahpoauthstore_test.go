package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/ahprepo"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
)

func TestAHPOAuthStoreRejectsUnboundLookups(t *testing.T) {
	t.Parallel()
	store := NewPostgresAHPOAuthStore(nil)
	_, err := store.GetConnection(context.Background(), "organization-1", "user:user-1", "client-1")
	require.ErrorIs(t, err, platformoauth.ErrNotFound)
	_, err = store.GetBoundConnection(context.Background(), "organization-1", "user:user-1", "client-1", "invalid", "https://example.com/ahp")
	require.ErrorIs(t, err, platformoauth.ErrNotFound)
	_, err = store.GetActiveSession(context.Background(), "organization-1", "jti-1")
	require.ErrorIs(t, err, platformoauth.ErrNotFound)
	require.ErrorIs(t, store.RegisterConnection(context.Background(), platformoauth.Connection{}), platformoauth.ErrNotFound)
	_, err = store.AuthorizeConnection(context.Background(), platformoauth.AuthorizeConnectionInput{})
	require.ErrorIs(t, err, platformoauth.ErrNotFound)
}

func TestAHPConnectionProjectionRetainsBinding(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	row := ahprepo.AhpConnection{ID: uuid.New(), ProjectID: projectID, ResourceUri: "https://example.com/ahp/project", OrganizationID: "organization-1", SubjectUrn: "user:user-1", ActiveGeneration: uuid.New(), AuthorizationExpiresAt: timestamp(time.Now().Add(time.Hour))}
	connection := ahpConnectionFromRow(row, "client-1")
	require.Equal(t, projectID.String(), connection.ProjectID)
	require.Equal(t, row.ResourceUri, connection.ResourceURI)
	require.Equal(t, row.OrganizationID, connection.OrganizationID)
	require.Equal(t, row.ActiveGeneration.String(), connection.Generation)
}

func TestAHPSessionProjectionsRetainBinding(t *testing.T) {
	t.Parallel()
	projectID := uuid.New()
	resource := "https://example.com/ahp/project"
	row := ahprepo.GetAHPSessionForRefreshRow{ID: uuid.New(), ProjectID: projectID, ResourceUri: resource, OrganizationID: "organization-1", ConnectionID: uuid.New(), ConnectionGeneration: uuid.New(), ClientID: "client-1"}
	session := ahpSessionFromRow(row)
	require.Equal(t, projectID.String(), session.Connection.ProjectID)
	require.Equal(t, resource, session.Connection.ResourceURI)
	locked := ahprepo.GetAHPSessionForRefreshForUpdateRow{ID: row.ID, ProjectID: projectID, ResourceUri: resource, OrganizationID: row.OrganizationID, ConnectionID: row.ConnectionID, ConnectionGeneration: row.ConnectionGeneration, ClientID: row.ClientID}
	require.Equal(t, session.Connection, ahpSessionFromRefreshRow(locked).Connection)
}

func TestAHPGrantValidationRetainsBinding(t *testing.T) {
	t.Parallel()
	now := time.Now()
	verifier := strings.Repeat("a", 43)
	digest := sha256.Sum256([]byte(verifier))
	generation := uuid.New()
	row := ahpGrantRow{ID: uuid.New(), ProjectID: uuid.NewString(), ResourceUri: "https://example.com/ahp/project", OrganizationID: "organization-1", ConnectionID: uuid.New(), ConnectionGeneration: generation, ActiveGeneration: generation, ClientID: "client-1", RedirectURI: "https://example.com/callback", CodeChallenge: base64.RawURLEncoding.EncodeToString(digest[:]), ExpiresAt: timestamp(now.Add(time.Minute)), AuthorizationExpiresAt: timestamp(now.Add(time.Hour))}
	input := platformoauth.ConsumeGrantInput{OrganizationID: row.OrganizationID, Code: "code", ClientID: row.ClientID, RedirectURI: row.RedirectURI, CodeVerifier: verifier, Now: now}
	grant, err := ahpValidateGrantRow(input, row)
	require.NoError(t, err)
	require.Equal(t, row.ProjectID, grant.Connection.ProjectID)
	require.Equal(t, row.ResourceUri, grant.Connection.ResourceURI)
	row.ConsumedAt = pgtype.Timestamptz{Time: now, Valid: true}
	_, err = ahpValidateGrantRow(input, row)
	require.ErrorIs(t, err, platformoauth.ErrAlreadyUsed)
	row.ConsumedAt = pgtype.Timestamptz{}
	row.ActiveGeneration = uuid.New()
	_, err = ahpValidateGrantRow(input, row)
	require.ErrorIs(t, err, platformoauth.ErrGeneration)
}
