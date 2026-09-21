package policyflags

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/feature"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	environment, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = environment
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("clean up test infrastructure: %v", err)
	}
	os.Exit(code)
}

type countingProvider struct {
	feature.Provider
	calls atomic.Int32
}

func (p *countingProvider) IsFlagEnabled(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (bool, error) {
	p.calls.Add(1)
	on, err := p.Provider.IsFlagEnabled(ctx, flag, distinctID, groups)
	if err != nil {
		return false, fmt.Errorf("counting provider: %w", err)
	}
	return on, nil
}

func TestProjectFlagStateResolvesEachFlagOncePerRequest(t *testing.T) {
	t.Parallel()
	db, err := infra.CloneTestDatabase(t, "policyflagstest")
	require.NoError(t, err)
	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Flags Example", Slug: orgID, WorkosID: pgtype.Text{String: orgID, Valid: true}, Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "Flags Example", Slug: "example", OrganizationID: orgID})
	require.NoError(t, err)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagRiskLLMAnalyzer, orgID, true)
	provider := &countingProvider{Provider: flags}
	logger := testenv.NewLogger(t)
	queries := repo.New(db)

	ctx := WithRequestMemo(t.Context())
	for range 3 {
		on, slug := ProjectFlagState(ctx, logger, queries, provider, orgID, project.ID, feature.FlagRiskLLMAnalyzer)
		require.True(t, on)
		require.Equal(t, orgID, slug)
	}
	require.EqualValues(t, 1, provider.calls.Load())
	require.False(t, ProjectFlagEnabled(ctx, logger, queries, provider, orgID, project.ID, feature.FlagRiskEnforcementPubsub))
	require.False(t, ProjectFlagEnabled(ctx, logger, queries, provider, orgID, project.ID, feature.FlagRiskEnforcementPubsub))
	require.EqualValues(t, 2, provider.calls.Load())

	require.True(t, ProjectFlagEnabled(t.Context(), logger, queries, provider, orgID, project.ID, feature.FlagRiskLLMAnalyzer))
	require.True(t, ProjectFlagEnabled(t.Context(), logger, queries, provider, orgID, project.ID, feature.FlagRiskLLMAnalyzer))
	require.EqualValues(t, 4, provider.calls.Load())

	// Concurrent scans sharing one memo still resolve each flag once.
	provider.calls.Store(0)
	ctx = WithRequestMemo(t.Context())
	var group sync.WaitGroup
	for range 8 {
		for _, flag := range []feature.Flag{feature.FlagRiskLLMAnalyzer, feature.FlagRiskEnforcementPubsub} {
			group.Go(func() { _ = ProjectFlagEnabled(ctx, logger, queries, provider, orgID, project.ID, flag) })
		}
	}
	group.Wait()
	require.EqualValues(t, 2, provider.calls.Load())

	// A failed lookup is not remembered; the next scan retries it.
	failing := &countingProvider{Provider: failingProvider{}}
	ctx = WithRequestMemo(t.Context())
	require.False(t, ProjectFlagEnabled(ctx, logger, queries, failing, orgID, project.ID, feature.FlagRiskLLMAnalyzer))
	require.False(t, ProjectFlagEnabled(ctx, logger, queries, failing, orgID, project.ID, feature.FlagRiskLLMAnalyzer))
	require.EqualValues(t, 2, failing.calls.Load())
}

type failingProvider struct{ feature.Provider }

func (failingProvider) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, errors.New("flag service unavailable")
}
