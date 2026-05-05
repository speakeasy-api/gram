package usersessions_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/user_session_consents"
	issuersgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func TestListUserSessionConsents(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "list-consents-issuer",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "list-consents-client")
	require.NoError(t, err)

	for _, principal := range []urn.SessionSubject{urn.NewUserSubject("p1"), urn.NewUserSubject("p2")} {
		_, err := seedUserSessionConsent(t, ctx, ti.conn, client.ID, principal)
		require.NoError(t, err)
	}

	got, err := ti.service.ListUserSessionConsents(ctx, &gen.ListUserSessionConsentsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:        nil,
		UserSessionClientID: nil,
		UserSessionIssuerID: nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 2)
}

func TestListUserSessionConsents_FilterByPrincipal(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "consents-by-principal",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "principal-filter-client")
	require.NoError(t, err)

	_, err = seedUserSessionConsent(t, ctx, ti.conn, client.ID, urn.NewUserSubject("keep"))
	require.NoError(t, err)
	_, err = seedUserSessionConsent(t, ctx, ti.conn, client.ID, urn.NewUserSubject("skip"))
	require.NoError(t, err)

	target := "user:keep"
	got, err := ti.service.ListUserSessionConsents(ctx, &gen.ListUserSessionConsentsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:        &target,
		UserSessionClientID: nil,
		UserSessionIssuerID: nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.Equal(t, target, got.Items[0].SubjectUrn)
}

func TestListUserSessionConsents_FilterByClient(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "consents-by-client",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	clientA, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "client-A")
	require.NoError(t, err)
	clientB, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "client-B")
	require.NoError(t, err)

	_, err = seedUserSessionConsent(t, ctx, ti.conn, clientA.ID, urn.NewUserSubject("a"))
	require.NoError(t, err)
	_, err = seedUserSessionConsent(t, ctx, ti.conn, clientB.ID, urn.NewUserSubject("b"))
	require.NoError(t, err)

	clientFilter := clientA.ID.String()
	got, err := ti.service.ListUserSessionConsents(ctx, &gen.ListUserSessionConsentsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:        nil,
		UserSessionClientID: &clientFilter,
		UserSessionIssuerID: nil,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.Equal(t, clientA.ID.String(), got.Items[0].UserSessionClientID)
}

func TestListUserSessionConsents_FilterByIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuerA, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "consents-issuer-a",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)
	issuerB, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "consents-issuer-b",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	clientA, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuerA.ID), "issuer-a-client")
	require.NoError(t, err)
	clientB, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuerB.ID), "issuer-b-client")
	require.NoError(t, err)

	_, err = seedUserSessionConsent(t, ctx, ti.conn, clientA.ID, urn.NewUserSubject("a"))
	require.NoError(t, err)
	_, err = seedUserSessionConsent(t, ctx, ti.conn, clientB.ID, urn.NewUserSubject("b"))
	require.NoError(t, err)

	issuerFilter := issuerA.ID
	got, err := ti.service.ListUserSessionConsents(ctx, &gen.ListUserSessionConsentsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:        nil,
		UserSessionClientID: nil,
		UserSessionIssuerID: &issuerFilter,
		Cursor:              nil,
		Limit:               nil,
	})
	require.NoError(t, err)
	require.Len(t, got.Items, 1)
	require.Equal(t, clientA.ID.String(), got.Items[0].UserSessionClientID)
}

func TestListUserSessionConsents_Pagination(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	issuer, err := ti.service.CreateUserSessionIssuer(ctx, &issuersgen.CreateUserSessionIssuerPayload{
		SessionToken:       nil,
		ApikeyToken:        nil,
		ProjectSlugInput:   nil,
		Slug:               "consents-pagination",
		AuthnChallengeMode: "chain",
		SessionDurationHours: 24,
	})
	require.NoError(t, err)

	client, err := seedUserSessionClient(t, ctx, ti.conn, uuid.MustParse(issuer.ID), "pagination-client")
	require.NoError(t, err)

	for _, principal := range []urn.SessionSubject{urn.NewUserSubject("p1"), urn.NewUserSubject("p2"), urn.NewUserSubject("p3")} {
		_, err := seedUserSessionConsent(t, ctx, ti.conn, client.ID, principal)
		require.NoError(t, err)
	}

	limit := 2
	page1, err := ti.service.ListUserSessionConsents(ctx, &gen.ListUserSessionConsentsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:        nil,
		UserSessionClientID: nil,
		UserSessionIssuerID: nil,
		Cursor:              nil,
		Limit:               &limit,
	})
	require.NoError(t, err)
	require.Len(t, page1.Items, 2)
	require.NotNil(t, page1.NextCursor)

	page2, err := ti.service.ListUserSessionConsents(ctx, &gen.ListUserSessionConsentsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:        nil,
		UserSessionClientID: nil,
		UserSessionIssuerID: nil,
		Cursor:              page1.NextCursor,
		Limit:               &limit,
	})
	require.NoError(t, err)
	require.Len(t, page2.Items, 1)
}

func TestListUserSessionConsents_RBACForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	ctx = withExactAuthzGrants(t, ctx, ti.conn)

	_, err := ti.service.ListUserSessionConsents(ctx, &gen.ListUserSessionConsentsPayload{
		SessionToken:        nil,
		ApikeyToken:         nil,
		ProjectSlugInput:    nil,
		SubjectUrn:        nil,
		UserSessionClientID: nil,
		UserSessionIssuerID: nil,
		Cursor:              nil,
		Limit:               nil,
	})
	requireOopsCode(t, err, oops.CodeForbidden)
}
