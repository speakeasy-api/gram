package platformmcp

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/ahprepo"
	platformoauth "github.com/speakeasy-api/gram/server/internal/platformmcp/oauth"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestAHPOAuthStoreDatabaseLifecycle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	db, err := platformMCPInfra.CloneTestDatabase(t, "ahp_oauth_lifecycle")
	require.NoError(t, err)
	principal, firstProject := seedRegistrationLifecycle(t, ctx, db)
	secondProject, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Second OAuth project", Slug: "second-oauth-" + uuid.NewString()[:8], OrganizationID: principal.OrganizationID})
	require.NoError(t, err)
	_, foreignProject := seedRegistrationLifecycle(t, ctx, db)
	store := NewPostgresAHPOAuthStore(db)
	client := platformoauth.Client{ID: uuid.NewString(), Name: "AHP lifecycle client", RedirectURIs: []string{"https://example.com/callback"}}
	require.NoError(t, store.RegisterClient(ctx, client))
	mcp := NewPostgresOAuthStore(db)
	require.NoError(t, mcp.RegisterClient(ctx, client))
	now := time.Now().UTC()
	verifier := strings.Repeat("a", 43)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	authorize := func(projectID string) (platformoauth.Connection, platformoauth.ConsumeGrantInput) {
		t.Helper()
		connection := platformoauth.Connection{ID: uuid.NewString(), ClientID: client.ID, Subject: "user:" + principal.UserID, OrganizationID: principal.OrganizationID, ProjectID: projectID, ResourceURI: "https://example.com/ahp/" + projectID, Generation: uuid.NewString(), AuthorizationExpiresAt: now.Add(24 * time.Hour)}
		code := uuid.NewString()
		saved, err := store.AuthorizeConnection(ctx, platformoauth.AuthorizeConnectionInput{Connection: connection, Grant: platformoauth.Grant{Code: code, ClientID: client.ID, Connection: connection, RedirectURI: client.RedirectURIs[0], CodeChallenge: challenge, ExpiresAt: now.Add(time.Minute)}, Now: now})
		require.NoError(t, err)
		require.Equal(t, connection.ProjectID, saved.ProjectID)
		require.Equal(t, connection.ResourceURI, saved.ResourceURI)
		return saved, platformoauth.ConsumeGrantInput{OrganizationID: principal.OrganizationID, Code: code, ClientID: client.ID, RedirectURI: client.RedirectURIs[0], CodeVerifier: verifier, Now: now}
	}
	first, firstCode := authorize(firstProject.ID.String())
	second, secondCode := authorize(secondProject.ID.String())
	require.NotEqual(t, first.ID, second.ID)
	for _, expected := range []platformoauth.Connection{first, second} {
		found, err := store.GetBoundConnection(ctx, expected.OrganizationID, expected.Subject, client.ID, expected.ProjectID, expected.ResourceURI)
		require.NoError(t, err)
		require.Equal(t, expected.ID, found.ID)
	}
	newSession := func(connection platformoauth.Connection) platformoauth.Session {
		return platformoauth.Session{ID: uuid.NewString(), ClientID: client.ID, Connection: connection, JTI: uuid.NewString(), RefreshHash: opaqueHash(uuid.NewString()), ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(12 * time.Hour)}
	}
	firstSession, secondSession := newSession(first), newSession(second)
	swapped := firstSession
	swapped.Connection = second
	_, err = store.ExchangeGrant(ctx, platformoauth.ExchangeGrantInput{ConsumeGrantInput: firstCode, Session: swapped})
	require.ErrorIs(t, err, platformoauth.ErrClientMismatch)
	swapped = firstSession
	swapped.Connection.ProjectID = second.ProjectID
	_, err = store.ExchangeGrant(ctx, platformoauth.ExchangeGrantInput{ConsumeGrantInput: firstCode, Session: swapped})
	require.ErrorIs(t, err, platformoauth.ErrClientMismatch)
	for _, item := range []struct {
		code    platformoauth.ConsumeGrantInput
		session platformoauth.Session
	}{{firstCode, firstSession}, {secondCode, secondSession}} {
		grant, err := store.ExchangeGrant(ctx, platformoauth.ExchangeGrantInput{ConsumeGrantInput: item.code, Session: item.session})
		require.NoError(t, err)
		require.Equal(t, item.session.Connection.ProjectID, grant.Connection.ProjectID)
		require.Equal(t, item.session.Connection.ResourceURI, grant.Connection.ResourceURI)
		active, err := store.GetActiveSession(ctx, principal.OrganizationID, item.session.JTI)
		require.NoError(t, err)
		require.Equal(t, item.session.Connection.ProjectID, active.Connection.ProjectID)
		require.Equal(t, item.session.Connection.ResourceURI, active.Connection.ResourceURI)
	}
	_, err = store.ExchangeGrant(ctx, platformoauth.ExchangeGrantInput{ConsumeGrantInput: firstCode, Session: newSession(first)})
	require.ErrorIs(t, err, platformoauth.ErrAlreadyUsed)
	prepared, err := store.PrepareRefresh(ctx, platformoauth.PrepareRefreshInput{OrganizationID: principal.OrganizationID, RefreshHash: firstSession.RefreshHash, ClientID: client.ID, Now: now})
	require.NoError(t, err)
	require.Equal(t, first.ProjectID, prepared.Connection.ProjectID)
	require.Equal(t, first.ResourceURI, prepared.Connection.ResourceURI)
	replacement := newSession(prepared.Connection)
	_, err = store.RotateSession(ctx, platformoauth.RotateSessionInput{OrganizationID: principal.OrganizationID, RefreshHash: firstSession.RefreshHash, ClientID: client.ID, Generation: first.Generation, Now: now, Replacement: secondSession})
	require.ErrorIs(t, err, platformoauth.ErrGeneration)
	_, err = store.RotateSession(ctx, platformoauth.RotateSessionInput{OrganizationID: principal.OrganizationID, RefreshHash: firstSession.RefreshHash, ClientID: client.ID, Generation: first.Generation, Now: now, Replacement: replacement})
	require.NoError(t, err)
	refreshed, err := store.GetSessionByRefreshHash(ctx, principal.OrganizationID, replacement.RefreshHash)
	require.NoError(t, err)
	require.Equal(t, first.ProjectID, refreshed.Connection.ProjectID)
	require.Equal(t, first.ResourceURI, refreshed.Connection.ResourceURI)
	_, err = store.GetActiveSession(ctx, principal.OrganizationID, firstSession.JTI)
	require.ErrorIs(t, err, platformoauth.ErrNotFound)

	// Composite ownership rejects an organization referencing another organization's project.
	invalid := first
	invalid.ID, invalid.ProjectID, invalid.Generation = uuid.NewString(), foreignProject.ID.String(), uuid.NewString()
	err = store.RegisterConnection(ctx, invalid)
	var pgerr *pgconn.PgError
	require.ErrorAs(t, err, &pgerr)
	require.Equal(t, "23503", pgerr.Code)
	// Grant and session project IDs must agree with their owning connection.
	queries := ahprepo.New(db)
	err = queries.FixtureRebindAHPGrantProject(ctx, ahprepo.FixtureRebindAHPGrantProjectParams{
		ReplacementProjectID: secondProject.ID, OrganizationID: principal.OrganizationID,
		ProjectID: firstProject.ID, ConnectionID: uuid.MustParse(first.ID),
	})
	require.ErrorAs(t, err, &pgerr)
	require.Equal(t, "23503", pgerr.Code)
	err = queries.FixtureRebindAHPSessionProject(ctx, ahprepo.FixtureRebindAHPSessionProjectParams{
		ReplacementProjectID: secondProject.ID, OrganizationID: principal.OrganizationID,
		ProjectID: firstProject.ID, ConnectionID: uuid.MustParse(first.ID),
	})
	require.ErrorAs(t, err, &pgerr)
	require.Equal(t, "23503", pgerr.Code)
	reused, err := store.DetectRefreshReuse(ctx, principal.OrganizationID, firstSession.RefreshHash, now)
	require.NoError(t, err)
	require.True(t, reused)
	_, err = store.GetActiveSession(ctx, principal.OrganizationID, replacement.JTI)
	require.ErrorIs(t, err, platformoauth.ErrNotFound)
	_, err = store.GetActiveSession(ctx, principal.OrganizationID, secondSession.JTI)
	require.NoError(t, err)
	require.NoError(t, store.RevokeConnection(ctx, principal.OrganizationID, first.ID, now))
	_, err = store.GetActiveSession(ctx, principal.OrganizationID, secondSession.JTI)
	require.NoError(t, err)
	require.NoError(t, store.RevokeClient(ctx, client.ID, now))
	_, err = store.GetActiveSession(ctx, principal.OrganizationID, secondSession.JTI)
	require.ErrorIs(t, err, platformoauth.ErrNotFound)
	_, err = mcp.GetClient(ctx, client.ID)
	require.NoError(t, err)
}

func TestAHPOAuthStoreNullableAuthorizationExpiry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		authorizedAge   time.Duration
		reauthorizedAge *time.Duration
		active          bool
	}{
		{name: "authorized_valid", authorizedAge: 89 * 24 * time.Hour, active: true},
		{name: "authorized_expired", authorizedAge: 91 * 24 * time.Hour},
		{name: "reauthorized_valid", authorizedAge: 180 * 24 * time.Hour, reauthorizedAge: new(89 * 24 * time.Hour), active: true},
		{name: "reauthorized_expired", authorizedAge: 180 * 24 * time.Hour, reauthorizedAge: new(91 * 24 * time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db, err := platformMCPInfra.CloneTestDatabase(t, "ahp_nullable_expiry")
			require.NoError(t, err)
			principal, project := seedRegistrationLifecycle(t, ctx, db)
			store := NewPostgresAHPOAuthStore(db)
			client := platformoauth.Client{ID: uuid.NewString(), Name: "AHP expiry client", RedirectURIs: []string{"https://example.com/callback"}}
			require.NoError(t, store.RegisterClient(ctx, client))
			now := time.Now().UTC()
			connection := platformoauth.Connection{ID: uuid.NewString(), ClientID: client.ID, Subject: "user:" + principal.UserID, OrganizationID: principal.OrganizationID, ProjectID: project.ID.String(), ResourceURI: "https://example.com/ahp/" + project.ID.String(), Generation: uuid.NewString(), AuthorizationExpiresAt: now.Add(time.Hour)}
			require.NoError(t, store.RegisterConnection(ctx, connection))
			session := platformoauth.Session{ID: uuid.NewString(), ClientID: client.ID, Connection: connection, JTI: uuid.NewString(), RefreshHash: opaqueHash(uuid.NewString()), ExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(time.Hour)}
			require.NoError(t, store.CreateSession(ctx, session))
			reauthorizedAt := pgtype.Timestamptz{}
			if tc.reauthorizedAge != nil {
				reauthorizedAt = timestamp(now.Add(-*tc.reauthorizedAge))
			}
			require.NoError(t, ahprepo.New(db).FixtureSetAHPAuthorizationTimes(ctx, ahprepo.FixtureSetAHPAuthorizationTimesParams{
				OrganizationID: principal.OrganizationID, ProjectID: project.ID, ConnectionID: uuid.MustParse(connection.ID),
				AuthorizedAt: timestamp(now.Add(-tc.authorizedAge)), ReauthorizedAt: reauthorizedAt,
			}))
			active, err := store.GetActiveSession(ctx, principal.OrganizationID, session.JTI)
			if tc.active {
				require.NoError(t, err)
				require.Equal(t, connection.ProjectID, active.Connection.ProjectID)
				require.Equal(t, connection.ResourceURI, active.Connection.ResourceURI)
			} else {
				require.ErrorIs(t, err, platformoauth.ErrNotFound)
			}
			_, err = store.GetActiveSession(ctx, "other-organization", session.JTI)
			require.ErrorIs(t, err, platformoauth.ErrNotFound)
		})
	}
}
