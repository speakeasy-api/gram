package remotesessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	orgclientsgen "github.com/speakeasy-api/gram/server/gen/organization_remote_session_clients"
	clientsgen "github.com/speakeasy-api/gram/server/gen/remote_session_clients"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestUpdateClient_CorrectsClientID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "correct-cid-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "correct-cid-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "mistyped-cid")
	subject := urn.NewUserSubject("correct-cid-user")
	insertRemoteSession(t, ctx, ti.conn, subject, userIssuerID.String(), clientID)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)

	corrected := "  corrected-cid  "
	updated, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		ID:           clientID,
		ClientID:     &corrected,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Equal(t, clientID, updated.ID, "the row is corrected in place")
	require.Equal(t, "corrected-cid", updated.ClientID)
	require.Equal(t, []string{userIssuerID.String()}, updated.UserSessionIssuerIds, "issuer bindings survive")

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionRemoteSessionClientUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)

	_, err = repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: uuid.MustParse(clientID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "sessions obtained through the old client_id are revoked")
}

func TestUpdateClient_UnchangedClientIDKeepsSessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "same-cid-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "same-cid-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "same-cid")
	subject := urn.NewUserSubject("same-cid-user")
	insertRemoteSession(t, ctx, ti.conn, subject, userIssuerID.String(), clientID)

	same := "same-cid"
	_, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		ID:           clientID,
		ClientID:     &same,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)

	_, err = repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: uuid.MustParse(clientID),
	})
	require.NoError(t, err, "resubmitting the stored client_id is not a correction")
}

func TestUpdateClient_RejectsEmptyClientID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "empty-cid-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "empty-cid-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "empty-cid")

	blank := "   "
	_, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		ID:           clientID,
		ClientID:     &blank,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateRemoteSessionClient_CorrectsClientID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "project-correct-cid-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "project-correct-cid-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "project-mistyped-cid")
	subject := urn.NewUserSubject("project-correct-cid-user")
	insertRemoteSession(t, ctx, ti.conn, subject, userIssuerID.String(), clientID)

	corrected := "project-corrected-cid"
	updated, err := ti.service.UpdateRemoteSessionClient(ctx, &clientsgen.UpdateRemoteSessionClientPayload{
		ID:               clientID,
		ClientID:         &corrected,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, corrected, updated.ClientID)

	_, err = repo.New(ti.conn).GetActiveRemoteSession(ctx, repo.GetActiveRemoteSessionParams{
		SubjectUrn:            subject,
		RemoteSessionClientID: uuid.MustParse(clientID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "sessions obtained through the old client_id are revoked")
}

func TestUpdateRemoteSessionClient_RefusesCIMDClientIDChange(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createCIMDIssuer(t, ctx, ti, "cimd-correct-cid", "https://idp.example.com/authorize", "https://idp.example.com/token")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "cimd-correct-cid-usi")
	created := createCimdClient(t, ctx, ti, issuerID.String(), userIssuerID.String(), nil)

	other := "https://example.com/not-the-document"
	_, err := ti.service.UpdateRemoteSessionClient(ctx, &clientsgen.UpdateRemoteSessionClientPayload{
		ID:               created.ID,
		ClientID:         &other,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestUpdateClient_DisplayName(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerID := createRemoteIssuer(t, ctx, ti, "display-name-issuer", "")
	userIssuerID := createUserSessionIssuer(t, ctx, ti.conn, "display-name-usi")
	clientID := createRemoteClient(t, ctx, ti, issuerID, userIssuerID.String(), "display-name-cid")

	got, err := ti.service.GetClient(ctx, &orgclientsgen.GetClientPayload{ID: clientID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)
	require.Nil(t, got.DisplayName, "a new client has no display name")

	name := "  Production login  "
	named, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		ID:           clientID,
		DisplayName:  &name,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.NotNil(t, named.DisplayName)
	require.Equal(t, "Production login", *named.DisplayName)

	// Omitting the field leaves the name alone.
	scoped, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		ID:           clientID,
		Scope:        []string{"openid"},
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.NotNil(t, scoped.DisplayName)
	require.Equal(t, "Production login", *scoped.DisplayName)

	// An empty name clears it, so the client falls back to its client_id.
	empty := " "
	cleared, err := ti.service.UpdateClient(ctx, &orgclientsgen.UpdateClientPayload{
		ID:           clientID,
		DisplayName:  &empty,
		SessionToken: nil,
		ApikeyToken:  nil,
	})
	require.NoError(t, err)
	require.Nil(t, cleared.DisplayName)
}
