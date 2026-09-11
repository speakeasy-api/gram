package rag

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newVectorSearchTestDatabase(t *testing.T) (*pgxpool.Pool, uuid.UUID, string) {
	t.Helper()

	ctx := t.Context()
	infra, cleanup, err := testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })

	conn, err := infra.CloneTestDatabase(t, "rag_filtered_vector_search")
	require.NoError(t, err)

	const organizationID = "org_rag_search_test"
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        "RAG Search Test Organization",
		Slug:        "rag-search-test-organization",
		WorkosID:    conv.ToPGText(organizationID),
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "RAG Search Test Project",
		Slug:           "rag-search-test-project",
		OrganizationID: organizationID,
	})
	require.NoError(t, err)

	config := conn.Config()
	config.ConnConfig.RuntimeParams["enable_seqscan"] = "off"
	config.ConnConfig.RuntimeParams["enable_bitmapscan"] = "off"
	config.ConnConfig.RuntimeParams["enable_sort"] = "off"
	config.ConnConfig.RuntimeParams["hnsw.iterative_scan"] = "off"
	forcedIndexConn, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	t.Cleanup(forcedIndexConn.Close)

	return forcedIndexConn, project.ID, organizationID
}
