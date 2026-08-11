package usersessions_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	sessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestListUserSessionClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "list-clients-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	issuerID := uuid.MustParse(issuer.ID)
	for _, name := range []string{"c1", "c2"} {
		_, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, name)
		require.NoError(t, err)
	}

	got, err := ti.service.ListUserSessionClients(ctx, &gen.ListUserSessionClientsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 2)
}

func TestListUserSessionClients_FilterByIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerA, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "client-filter-a",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	issuerB, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "client-filter-b",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	_, err = seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuerA.ID), "client-a-1")
	require.NoError(t, err)
	_, err = seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuerB.ID), "client-b-1")
	require.NoError(t, err)

	filter := issuerA.ID
	got, err := ti.service.ListUserSessionClients(ctx, &gen.ListUserSessionClientsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: &filter,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.Equal(t, issuerA.ID, got.Items[0].UserSessionIssuerID)
}

// client_id_metadata_uri is the CIMD/DCR discriminator the dashboard renders
// its source badge from, so the view has to carry it faithfully in both
// directions: set (and equal to client_id) for a CIMD row, nil for a DCR row.
func TestListUserSessionClients_ClientIDMetadataURI(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "cimd-source-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	issuerID := uuid.MustParse(issuer.ID)

	dcrClient, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "dcr-client")
	require.NoError(t, err)

	const documentURL = "https://client.example.com/oauth-client.json"
	cimdClient, err := seedCimdUserSessionClient(t, ctx, ti.conn, issuerID, documentURL)
	require.NoError(t, err)

	got, err := ti.service.ListUserSessionClients(ctx, &gen.ListUserSessionClientsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: &issuer.ID,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 2)

	byID := make(map[string]*types.UserSessionClient, len(got.Items))
	for _, item := range got.Items {
		byID[item.ID] = item
	}

	gotDCR := byID[dcrClient.ID.String()]
	require.NotNil(t, gotDCR)
	require.Nil(t, gotDCR.ClientIDMetadataURI, "a DCR row must not advertise a metadata document")

	gotCIMD := byID[cimdClient.ID.String()]
	require.NotNil(t, gotCIMD)
	require.NotNil(t, gotCIMD.ClientIDMetadataURI)
	require.Equal(t, documentURL, *gotCIMD.ClientIDMetadataURI)
	require.Equal(t, gotCIMD.ClientID, *gotCIMD.ClientIDMetadataURI, "for a CIMD row the document URL is the client_id")
}

// The clients listing reports a live-session tally per registration, which the
// dashboard turns into a drill-down into that client's sessions. The tally has
// to agree with what the sessions listing's active filter would return, so
// expired and revoked sessions are excluded and a client that never issued one
// reports zero rather than being omitted.
func TestListUserSessionClients_ActiveSessionCount(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:         nil,
		ApikeyToken:          nil,
		ProjectSlugInput:     nil,
		Slug:                 "active-count-issuer",
		AuthnChallengeMode:   "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	issuerID := uuid.MustParse(issuer.ID)

	busyClient, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "busy-client")
	require.NoError(t, err)
	idleClient, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, "idle-client")
	require.NoError(t, err)

	_, err = seedUserSessionForClient(t, ctx, ti.conn, issuerID, busyClient.ID, urn.NewUserSubject("count-live"))
	require.NoError(t, err)

	revoked, err := seedUserSessionForClient(t, ctx, ti.conn, issuerID, busyClient.ID, urn.NewUserSubject("count-revoked"))
	require.NoError(t, err)
	require.NoError(t, ti.service.RevokeUserSession(ctx, &sessionsgen.RevokeUserSessionPayload{
		ID:               revoked.ID.String(),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}))

	busyClientID := uuid.NullUUID{UUID: busyClient.ID, Valid: true}
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(24 * time.Hour)

	// Active/expired is keyed off refresh_expires_at (the authorization
	// deadline), not expires_at (the ~1h access-token lifetime). This session
	// is the case that distinguishes the two: a live connection that has not
	// refreshed recently, so its access token has lapsed while its refresh
	// token has not. It has to count.
	_, err = seedUserSessionFull(t, ctx, ti.conn, issuerID, busyClientID, urn.NewUserSubject("count-between-refreshes"), past, future)
	require.NoError(t, err)

	// Genuinely past its authorization deadline, so it must not count.
	_, err = seedUserSessionFull(t, ctx, ti.conn, issuerID, busyClientID, urn.NewUserSubject("count-expired"), past, past)
	require.NoError(t, err)

	got, err := ti.service.ListUserSessionClients(ctx, &gen.ListUserSessionClientsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: &issuer.ID,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	// Asserted before the map lookups below: a client with no sessions must be
	// listed reporting zero, and a map miss would read as zero too.
	require.Len(t, got.Items, 2)

	counts := make(map[string]int, len(got.Items))
	for _, item := range got.Items {
		counts[item.ID] = item.ActiveSessionCount
	}
	// The live session plus the one between refreshes; the revoked and the
	// expired ones are excluded.
	require.Equal(t, 2, counts[busyClient.ID.String()])
	require.Equal(t, 0, counts[idleClient.ID.String()])
}

func TestListUserSessionClients_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.ListUserSessionClients(ctx, &gen.ListUserSessionClientsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: nil,
		Cursor:              nil,
		Limit:               nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
