package access

import (
	"context"
	"testing"

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

	return ctx, &Service{tracer: nil, logger: logger, db: conn, chConn: nil, auth: nil, authz: nil, roles: nil, featureCache: nil}, conn
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
