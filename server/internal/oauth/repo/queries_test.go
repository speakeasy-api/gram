package repo_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	oauthrepo "github.com/speakeasy-api/gram/server/internal/oauth/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func createOAuthProject(t *testing.T, ctx context.Context, conn *pgxpool.Pool) uuid.UUID {
	t.Helper()
	orgID := fmt.Sprintf("org-%s", uuid.NewString()[:8])
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Test Org", Slug: orgID, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Test Project", Slug: fmt.Sprintf("test-%s", uuid.NewString()[:8]), OrganizationID: orgID,
	})
	require.NoError(t, err)
	return project.ID
}

func TestCreateExternalOAuthServerMetadataSources(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	projectID := createOAuthProject(t, ctx, conn)
	queries := oauthrepo.New(conn)

	metadataRow, err := queries.CreateExternalOAuthServerMetadata(ctx, oauthrepo.CreateExternalOAuthServerMetadataParams{
		ProjectID: projectID, Slug: "metadata-source", Metadata: []byte(`{"issuer":"https://auth.example.com"}`),
	})
	require.NoError(t, err)
	require.NotNil(t, metadataRow.Metadata)
	require.False(t, metadataRow.AuthorizationServerIssuer.Valid)

	issuerRow, err := queries.CreateExternalOAuthServerMetadata(ctx, oauthrepo.CreateExternalOAuthServerMetadataParams{
		ProjectID: projectID, Slug: "issuer-source", AuthorizationServerIssuer: pgtype.Text{String: "https://auth.example.com", Valid: true},
	})
	require.NoError(t, err)
	require.Nil(t, issuerRow.Metadata)
	require.Equal(t, "https://auth.example.com", issuerRow.AuthorizationServerIssuer.String)
}

func TestCreateGeneratedExternalOAuthServerMetadataCollision(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	projectID := createOAuthProject(t, ctx, conn)
	queries := oauthrepo.New(conn)
	params := oauthrepo.CreateGeneratedExternalOAuthServerMetadataParams{ProjectID: projectID, Slug: "toolset-oauth", Metadata: []byte(`{}`)}

	_, err = queries.CreateGeneratedExternalOAuthServerMetadata(ctx, params)
	require.NoError(t, err)
	_, err = queries.CreateGeneratedExternalOAuthServerMetadata(ctx, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	issuerRow, err := queries.CreateGeneratedExternalOAuthServerMetadata(ctx, oauthrepo.CreateGeneratedExternalOAuthServerMetadataParams{
		ProjectID: projectID, Slug: "toolset-issuer-oauth", AuthorizationServerIssuer: pgtype.Text{String: "https://auth.example.com", Valid: true},
	})
	require.NoError(t, err)
	require.Nil(t, issuerRow.Metadata)
	require.True(t, issuerRow.AuthorizationServerIssuer.Valid)
}

func TestUpdateExternalOAuthServerSource(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	projectID := createOAuthProject(t, ctx, conn)
	otherProjectID := createOAuthProject(t, ctx, conn)
	queries := oauthrepo.New(conn)

	created, err := queries.CreateExternalOAuthServerMetadata(ctx, oauthrepo.CreateExternalOAuthServerMetadataParams{
		ProjectID: projectID, Slug: "update-source", Metadata: []byte(`{"issuer":"https://auth.example.com"}`),
	})
	require.NoError(t, err)
	updated, err := queries.UpdateExternalOAuthServerSource(ctx, oauthrepo.UpdateExternalOAuthServerSourceParams{
		ProjectID: projectID, ID: created.ID, AuthorizationServerIssuer: pgtype.Text{String: "https://auth.example.com", Valid: true},
	})
	require.NoError(t, err)
	require.Nil(t, updated.Metadata)
	require.True(t, updated.AuthorizationServerIssuer.Valid)

	_, err = queries.UpdateExternalOAuthServerSource(ctx, oauthrepo.UpdateExternalOAuthServerSourceParams{
		ProjectID: otherProjectID, ID: created.ID, Metadata: []byte(`{}`),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = queries.UpdateExternalOAuthServerSource(ctx, oauthrepo.UpdateExternalOAuthServerSourceParams{
		ProjectID: projectID, ID: created.ID, Metadata: []byte(`{}`), AuthorizationServerIssuer: pgtype.Text{String: "https://auth.example.com", Valid: true},
	})
	require.Error(t, err)

	unchanged, err := queries.GetExternalOAuthServerMetadata(ctx, oauthrepo.GetExternalOAuthServerMetadataParams{ProjectID: projectID, ID: created.ID})
	require.NoError(t, err)
	require.Nil(t, unchanged.Metadata)
	require.Equal(t, "https://auth.example.com", unchanged.AuthorizationServerIssuer.String)

	cleared, err := queries.UpdateExternalOAuthServerSource(ctx, oauthrepo.UpdateExternalOAuthServerSourceParams{
		ProjectID: projectID, ID: created.ID, Metadata: []byte(`{"issuer":"https://auth.example.com"}`),
	})
	require.NoError(t, err)
	require.NotNil(t, cleared.Metadata)
	require.False(t, cleared.AuthorizationServerIssuer.Valid)

	_, err = queries.GetExternalOAuthServerMetadata(ctx, oauthrepo.GetExternalOAuthServerMetadataParams{ProjectID: otherProjectID, ID: created.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
