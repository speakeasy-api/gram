package usersessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	clientsgen "github.com/speakeasy-api/gram/server/gen/user_session_clients"
	consentsgen "github.com/speakeasy-api/gram/server/gen/user_session_consents"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	cimdgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers_cimd_clients"
	sessionsgen "github.com/speakeasy-api/gram/server/gen/user_sessions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// The organization-tier predicate reads
//
//	project_id = @project_id OR (project_id IS NULL AND organization_id = @organization_id)
//
// and every row written today carries BOTH columns, so the project_id IS NULL
// guard is the only thing confining the second arm to genuine
// organization-tier rows. Drop it and the arm matches every sibling project's
// rows in the same organization, turning tenant isolation into a cross-project
// read -- and, on the revoke and delete paths, a cross-project write.
//
// These tests pin that boundary from the outside: a second project in the SAME
// organization owns a full user-session object graph, and the caller's project
// must not be able to see, list, count, revoke, or delete any of it. They are
// written to pass both before and after the organization-tier rescope; a
// predicate that loses the guard fails them.

// foreignProject is the object graph owned by a sibling project: an issuer and
// one of every child that hangs off it.
type foreignProject struct {
	projectID uuid.UUID
	issuerID  uuid.UUID
	clientID  uuid.UUID
	sessionID uuid.UUID
	consentID uuid.UUID
	cimdID    uuid.UUID
}

// seedForeignProject builds a sibling project in the caller's organization and
// fills it with a complete issuer subtree. The children take their tenancy
// from the issuer inside SQL, so seeding through the issuer is what makes them
// the sibling project's rows.
func seedForeignProject(t *testing.T, ctx context.Context, ti *testInstance, slug string) foreignProject {
	t.Helper()

	projectID := createSiblingProject(t, ctx, ti.conn, slug)
	issuerID := seedIssuerInProject(t, ctx, ti.conn, projectID, slug+"-issuer")

	client, err := seedUserSessionClient(t, ctx, ti.conn, issuerID, slug+"-client")
	require.NoError(t, err)

	session, err := seedUserSessionForClient(t, ctx, ti.conn, issuerID, client.ID, urn.NewUserSubject(slug+"-subject"))
	require.NoError(t, err)

	consent, err := seedUserSessionConsent(t, ctx, ti.conn, client.ID, urn.NewUserSubject(slug+"-subject"))
	require.NoError(t, err)

	cimd, err := repo.New(ti.conn).CreateUserSessionIssuerCimdClient(ctx, repo.CreateUserSessionIssuerCimdClientParams{
		ProjectID:           projectID,
		ClientIDMetadataUri: "https://" + slug + ".example.com/client",
		UserSessionIssuerID: issuerID,
	})
	require.NoError(t, err)

	return foreignProject{
		projectID: projectID,
		issuerID:  issuerID,
		clientID:  client.ID,
		sessionID: session.ID,
		consentID: consent.ID,
		cimdID:    cimd.ID,
	}
}

// requireForeignIssuerLive re-reads the sibling project's issuer under its own
// project id. A mutation that leaked across the boundary soft-deletes the row,
// so this is what turns a silent cross-project write into a failure.
func requireForeignIssuerLive(t *testing.T, ctx context.Context, ti *testInstance, fp foreignProject) {
	t.Helper()

	_, err := repo.New(ti.conn).GetUserSessionIssuerByID(ctx, repo.GetUserSessionIssuerByIDParams{
		ID:        fp.issuerID,
		ProjectID: fp.projectID,
	})
	require.NoError(t, err, "sibling project's issuer must survive the caller's mutations")
}

func TestCrossProjectIsolation_Issuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fp := seedForeignProject(t, ctx, ti, "iso-issuer")
	foreignID := fp.issuerID.String()

	_, err := ti.service.GetUserSessionIssuer(ctx, &issuersgen.GetUserSessionIssuerPayload{
		ID:               &foreignID,
		Slug:             nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	foreignSlug := "iso-issuer-issuer"
	_, err = ti.service.GetUserSessionIssuer(ctx, &issuersgen.GetUserSessionIssuerPayload{
		ID:               nil,
		Slug:             &foreignSlug,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	listed, err := ti.service.ListUserSessionIssuers(ctx, &issuersgen.ListUserSessionIssuersPayload{
		Cursor:           nil,
		Limit:            nil,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	for _, item := range listed.Items {
		require.NotEqual(t, foreignID, item.ID, "listing must not reach a sibling project's issuer")
	}

	renamed := "hijacked"
	_, err = ti.service.UpdateUserSessionIssuer(ctx, &issuersgen.UpdateUserSessionIssuerPayload{
		SessionToken:                  nil,
		ApikeyToken:                   nil,
		ProjectSlugInput:              nil,
		ID:                            foreignID,
		Slug:                          &renamed,
		AuthnChallengeMode:            nil,
		SessionDurationHours:          nil,
		ClientIDMetadataAdmissionMode: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	err = ti.service.DeleteUserSessionIssuer(ctx, &issuersgen.DeleteUserSessionIssuerPayload{
		ID:               foreignID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	requireForeignIssuerLive(t, ctx, ti, fp)
}

func TestCrossProjectIsolation_Clients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fp := seedForeignProject(t, ctx, ti, "iso-client")
	foreignClientID := fp.clientID.String()
	foreignIssuerID := fp.issuerID.String()

	_, err := ti.service.GetUserSessionClient(ctx, &clientsgen.GetUserSessionClientPayload{
		ID:               foreignClientID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	listed, err := ti.service.ListUserSessionClients(ctx, &clientsgen.ListUserSessionClientsPayload{
		UserSessionIssuerID: nil,
		Cursor:              nil,
		Limit:               nil,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	for _, item := range listed.Items {
		require.NotEqual(t, foreignClientID, item.ID, "listing must not reach a sibling project's client")
	}

	// Naming the sibling issuer explicitly is the sharper case: the filter is
	// satisfied, so only the project predicate keeps the row out.
	filtered, err := ti.service.ListUserSessionClients(ctx, &clientsgen.ListUserSessionClientsPayload{
		UserSessionIssuerID: &foreignIssuerID,
		Cursor:              nil,
		Limit:               nil,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	require.Empty(t, filtered.Items)

	err = ti.service.RevokeUserSessionClient(ctx, &clientsgen.RevokeUserSessionClientPayload{
		ID:               foreignClientID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	live, err := repo.New(ti.conn).GetUserSessionClientByID(ctx, repo.GetUserSessionClientByIDParams{
		ID:        fp.clientID,
		ProjectID: fp.projectID,
	})
	require.NoError(t, err, "sibling project's client must survive the caller's revoke")
	require.False(t, live.Deleted)
}

func TestCrossProjectIsolation_Sessions(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fp := seedForeignProject(t, ctx, ti, "iso-session")
	foreignSessionID := fp.sessionID.String()
	foreignIssuerID := fp.issuerID.String()

	listed, err := ti.service.ListUserSessions(ctx, &sessionsgen.ListUserSessionsPayload{
		SubjectUrn:          nil,
		UserSessionIssuerID: nil,
		Status:              nil,
		ClientID:            nil,
		Cursor:              nil,
		Limit:               nil,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	for _, item := range listed.Items {
		require.NotEqual(t, foreignSessionID, item.ID, "listing must not reach a sibling project's session")
	}

	filtered, err := ti.service.ListUserSessions(ctx, &sessionsgen.ListUserSessionsPayload{
		SubjectUrn:          nil,
		UserSessionIssuerID: &foreignIssuerID,
		Status:              nil,
		ClientID:            nil,
		Cursor:              nil,
		Limit:               nil,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	require.Empty(t, filtered.Items)

	// The three facet queries scope through the issuer the same way the
	// listing does, so a leaked predicate shows up as a sibling project's
	// issuer, client, or subject appearing in the caller's counts.
	facets, err := ti.service.ListFacets(ctx, &sessionsgen.ListFacetsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	for _, facet := range facets.Servers {
		require.NotEqual(t, foreignIssuerID, facet.Value, "server facets must not count a sibling project's issuer")
	}
	for _, facet := range facets.Clients {
		require.NotEqual(t, fp.clientID.String(), facet.Value, "client facets must not count a sibling project's client")
	}
	for _, facet := range facets.Users {
		require.NotEqual(t, "user:iso-session-subject", facet.Value, "user facets must not count a sibling project's subject")
	}

	err = ti.service.RevokeUserSession(ctx, &sessionsgen.RevokeUserSessionPayload{
		ID:               foreignSessionID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	live, err := repo.New(ti.conn).GetUserSessionByID(ctx, repo.GetUserSessionByIDParams{
		ID:        fp.sessionID,
		ProjectID: fp.projectID,
	})
	require.NoError(t, err, "sibling project's session must survive the caller's revoke")
	require.False(t, live.Deleted)
}

func TestCrossProjectIsolation_Consents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fp := seedForeignProject(t, ctx, ti, "iso-consent")
	foreignConsentID := fp.consentID.String()
	foreignIssuerID := fp.issuerID.String()

	listed, err := ti.service.ListUserSessionConsents(ctx, &consentsgen.ListUserSessionConsentsPayload{
		SubjectUrn:          nil,
		UserSessionClientID: nil,
		UserSessionIssuerID: nil,
		Cursor:              nil,
		Limit:               nil,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	for _, item := range listed.Items {
		require.NotEqual(t, foreignConsentID, item.ID, "listing must not reach a sibling project's consent")
	}

	filtered, err := ti.service.ListUserSessionConsents(ctx, &consentsgen.ListUserSessionConsentsPayload{
		SubjectUrn:          nil,
		UserSessionClientID: nil,
		UserSessionIssuerID: &foreignIssuerID,
		Cursor:              nil,
		Limit:               nil,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	require.Empty(t, filtered.Items)

	err = ti.service.RevokeUserSessionConsent(ctx, &consentsgen.RevokeUserSessionConsentPayload{
		ID:               foreignConsentID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	live, err := repo.New(ti.conn).GetUserSessionConsentByID(ctx, repo.GetUserSessionConsentByIDParams{
		ID:        fp.consentID,
		ProjectID: fp.projectID,
	})
	require.NoError(t, err, "sibling project's consent must survive the caller's revoke")
	require.False(t, live.Deleted)
}

func TestCrossProjectIsolation_CimdClients(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	fp := seedForeignProject(t, ctx, ti, "iso-cimd")
	foreignIssuerID := fp.issuerID.String()
	foreignCimdID := fp.cimdID.String()

	// The create path scopes its INSERT ... SELECT source to a live issuer in
	// the caller's project, so a sibling project's issuer yields no rows.
	_, err := ti.service.CreateUserSessionIssuerCimdClient(ctx, &cimdgen.CreateUserSessionIssuerCimdClientPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		UserSessionIssuerID: foreignIssuerID,
		ClientIDMetadataURI: "https://attacker.example.com/client",
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	_, err = ti.service.GetUserSessionIssuerCimdClient(ctx, &cimdgen.GetUserSessionIssuerCimdClientPayload{
		ID:               foreignCimdID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	listed, err := ti.service.ListUserSessionIssuerCimdClients(ctx, &cimdgen.ListUserSessionIssuerCimdClientsPayload{
		UserSessionIssuerID: foreignIssuerID,
		Cursor:              nil,
		Limit:               nil,
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
	})
	require.NoError(t, err)
	require.Empty(t, listed.Items)

	err = ti.service.DeleteUserSessionIssuerCimdClient(ctx, &cimdgen.DeleteUserSessionIssuerCimdClientPayload{
		ID:               foreignCimdID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)

	live, err := repo.New(ti.conn).GetUserSessionIssuerCimdClientByID(ctx, repo.GetUserSessionIssuerCimdClientByIDParams{
		ID:        fp.cimdID,
		ProjectID: fp.projectID,
	})
	require.NoError(t, err, "sibling project's CIMD grant must survive the caller's delete")
	require.False(t, live.Deleted)
}
