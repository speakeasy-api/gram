package testenv

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/internal/auth/sessions"
	"github.com/speakeasy-api/gram/internal/contextvalues"
	projectsRepo "github.com/speakeasy-api/gram/internal/projects/repo"
)

func InitAuthContext(t *testing.T, ctx context.Context, conn *pgxpool.Pool, sessionManager *sessions.Manager) context.Context {
	t.Helper()

	ctx, err := sessionManager.Authenticate(ctx, "", true)
	require.NoError(t, err)

	authctx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok, "auth context not found")

	p, err := projectsRepo.New(conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           "test-project",
		Slug:           "test-project",
		OrganizationID: authctx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	authctx.ProjectID = &p.ID

	return ctx
}
