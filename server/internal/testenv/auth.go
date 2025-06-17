package testenv

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
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

	// Generate unique project slug to avoid conflicts when tests run in parallel
	// Keep it short to comply with database constraint (max 40 chars)
	projectSlug := fmt.Sprintf("test-%s", uuid.New().String()[:8])

	p, err := projectsRepo.New(conn).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name:           projectSlug,
		Slug:           projectSlug,
		OrganizationID: authctx.ActiveOrganizationID,
	})
	require.NoError(t, err)

	authctx.ProjectID = &p.ID

	return ctx
}
