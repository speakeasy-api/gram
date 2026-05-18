package access

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func newInternalTestService(t *testing.T) (context.Context, *Service, *pgxpool.Pool) {
	t.Helper()

	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, cleanup())
	})

	ctx := t.Context()
	logger := testenv.NewLogger(t)

	conn, err := res.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	return ctx, &Service{tracer: nil, logger: logger, db: conn, chConn: nil, auth: nil, authz: nil, roleMgr: nil, featureCache: nil}, conn
}

func seedInternalOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string) {
	t.Helper()

	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:       organizationID,
		Name:     "Test Org",
		Slug:     "test-org",
		WorkosID: conv.PtrToPGText(nil),
	})
	require.NoError(t, err)
}

func seedInternalGrant(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, principal urn.Principal, scope string, resource string) {
	t.Helper()

	selectors, err := authz.NewSelector(authz.Scope(scope), resource).MarshalJSON()
	require.NoError(t, err)

	_, err = accessrepo.New(conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: organizationID,
		PrincipalUrn:   principal,
		Scope:          scope,
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func seedInternalRole(t *testing.T, ctx context.Context, conn *pgxpool.Pool, organizationID string, roleSlug string) urn.Principal {
	t.Helper()

	now := time.Now().UTC()
	row, err := accessrepo.New(conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    organizationID,
		WorkosSlug:        roleSlug,
		WorkosName:        roleSlug,
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)

	principal, err := urn.ParsePrincipal(row.RoleUrn)
	require.NoError(t, err)
	return principal
}
