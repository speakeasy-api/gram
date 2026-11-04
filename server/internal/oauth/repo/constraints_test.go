package repo_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oauth/repo"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}

	os.Exit(code)
}

func TestExternalOAuthServerMetadataSourceConstraint(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	orgID := fmt.Sprintf("org-%s", uuid.NewString()[:8])
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Test Org", Slug: orgID, WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)

	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Test Project", Slug: fmt.Sprintf("test-%s", uuid.NewString()[:8]), OrganizationID: orgID,
	})
	require.NoError(t, err)

	tests := []struct {
		name     string
		metadata []byte
		issuer   pgtype.Text
		valid    bool
	}{
		{name: "metadata only", metadata: []byte(`{}`), valid: true},
		{name: "issuer only", issuer: conv.ToPGText("https://auth.example.com"), valid: true},
		{name: "both", metadata: []byte(`{}`), issuer: conv.ToPGText("https://auth.example.com")},
		{name: "neither"},
		{name: "empty issuer", issuer: pgtype.Text{String: "", Valid: true}},
		{name: "issuer over 500 characters", issuer: conv.ToPGText(strings.Repeat("a", 501))},
		{name: "issuer at 500 characters", issuer: conv.ToPGText(strings.Repeat("a", 500)), valid: true},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := repo.New(conn).InsertExternalOAuthServerMetadataSourceFixture(ctx, repo.InsertExternalOAuthServerMetadataSourceFixtureParams{
				ProjectID:                 project.ID,
				Slug:                      fmt.Sprintf("server-%d", i),
				Metadata:                  tt.metadata,
				AuthorizationServerIssuer: tt.issuer,
			})
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
