package identityproviders_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/identity_providers"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/identityproviders/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteRemovesLiveConnectionAndSigningKeysAndAudits(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	connection := createConnection(t, ctx, ti, "https://acme.okta.com")
	connectionID := mustUUID(t, connection.ID)
	beforeAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionDeleted)
	require.NoError(t, err)

	err = ti.service.Delete(ctx, &gen.DeletePayload{ID: connection.ID, SessionToken: nil, ApikeyToken: nil})
	require.NoError(t, err)

	_, err = repo.New(ti.conn).GetIdentityProviderConnectionByOrganization(ctx, ti.orgID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = repo.New(ti.conn).GetIdentityProviderSigningKey(ctx, repo.GetIdentityProviderSigningKeyParams{
		OrganizationID:               ti.orgID,
		IdentityProviderConnectionID: connectionID,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = repo.New(ti.conn).GetIdentityProviderJSONWebKeySet(ctx, connectionID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	afterAudits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionDeleted)
	require.NoError(t, err)
	require.Equal(t, beforeAudits+1, afterAudits)
	record, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionIdentityProviderConnectionDeleted)
	require.NoError(t, err)
	require.Equal(t, ti.orgID, record.OrganizationID)
	require.Equal(t, connection.ID, record.SubjectID)
	require.Equal(t, "acme.okta.com", record.SubjectDisplay)
}

func TestDeleteRequiresOrganizationAdmin(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	ctx = authztest.WithExactGrants(t, ctx)

	err := ti.service.Delete(ctx, &gen.DeletePayload{ID: uuid.NewString(), SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestDeleteRejectsInvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.Delete(ctx, &gen.DeletePayload{ID: "not-a-uuid", SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestDeleteReturnsNotFoundForUnknownID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	err := ti.service.Delete(ctx, &gen.DeletePayload{ID: uuid.NewString(), SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestDeleteReturnsNotFoundForDifferentConnectionID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	createConnection(t, ctx, ti, "https://acme.okta.com")

	err := ti.service.Delete(ctx, &gen.DeletePayload{ID: uuid.NewString(), SessionToken: nil, ApikeyToken: nil})
	requireOopsCode(t, err, oops.CodeNotFound)
}
