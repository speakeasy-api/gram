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
	"github.com/speakeasy-api/gram/server/internal/risk/enforcereply"
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
	calls        atomic.Int32
	payloadCalls atomic.Int32
}

func (p *countingProvider) IsFlagEnabled(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (bool, error) {
	p.calls.Add(1)
	on, err := p.Provider.IsFlagEnabled(ctx, flag, distinctID, groups)
	if err != nil {
		return false, fmt.Errorf("counting provider: %w", err)
	}
	return on, nil
}

func (p *countingProvider) FlagPayload(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) ([]byte, error) {
	p.payloadCalls.Add(1)
	payload, err := p.Provider.FlagPayload(ctx, flag, distinctID, groups)
	if err != nil {
		return nil, fmt.Errorf("counting provider: %w", err)
	}
	return payload, nil
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

func newPayloadTestProject(t *testing.T, databaseName string) (string, uuid.UUID, *repo.Queries, *feature.InMemory, *countingProvider) {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, databaseName)
	require.NoError(t, err)
	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Payload Example", Slug: orgID, WorkosID: pgtype.Text{String: orgID, Valid: true}, Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "Payload Example", Slug: "payload", OrganizationID: orgID})
	require.NoError(t, err)
	flags := &feature.InMemory{}
	provider := &countingProvider{Provider: flags}
	return orgID, project.ID, repo.New(db), flags, provider
}

func TestProjectFlagPayloadResolvesEachFlagOncePerRequest(t *testing.T) {
	t.Parallel()
	orgID, projectID, queries, flags, provider := newPayloadTestProject(t, "policyflagpayloadmemo")
	flags.SetFlagPayload(feature.FlagRiskEnforcementMaxContentBytes, orgID, []byte(`{"max_content_bytes":2048}`))
	logger := testenv.NewLogger(t)
	ctx := WithRequestMemo(t.Context())

	for range 3 {
		payload, found := ProjectFlagPayload(ctx, logger, queries, provider, orgID, projectID, feature.FlagRiskEnforcementMaxContentBytes)
		require.True(t, found)
		require.JSONEq(t, `{"max_content_bytes":2048}`, string(payload))
	}
	require.EqualValues(t, 1, provider.payloadCalls.Load())

	for range 2 {
		payload, found := ProjectFlagPayload(ctx, logger, queries, provider, orgID, projectID, feature.FlagRiskEnforcementPubsub)
		require.False(t, found)
		require.Nil(t, payload)
	}
	require.EqualValues(t, 2, provider.payloadCalls.Load())
}

func TestEnforcementMaxContentBytesParsesAndClampsPayload(t *testing.T) {
	t.Parallel()
	orgID, projectID, queries, flags, _ := newPayloadTestProject(t, "policyflagpayloadlimits")
	logger := testenv.NewLogger(t)
	cases := []struct {
		payload string
		want    int
	}{
		{payload: `{"max_content_bytes":2048}`, want: 2048},
		{payload: `{"max_content_bytes":100}`, want: 1024},
		{payload: fmt.Sprintf(`{"max_content_bytes":%d}`, enforcereply.MaxContentBytes*2), want: enforcereply.MaxContentBytes},
	}
	for _, test := range cases {
		flags.SetFlagPayload(feature.FlagRiskEnforcementMaxContentBytes, orgID, []byte(test.payload))
		limit, source := EnforcementMaxContentBytes(t.Context(), logger, queries, flags, orgID, projectID)
		require.Equal(t, test.want, limit)
		require.Equal(t, "flag", source)
	}
}

func TestEnforcementMaxContentBytesDefaultsForInvalidPayload(t *testing.T) {
	t.Parallel()
	orgID, projectID, queries, flags, _ := newPayloadTestProject(t, "policyflagpayloadinvalid")
	logger := testenv.NewLogger(t)
	for _, payload := range []string{
		`{`,
		`{"max_content_bytes":-1}`,
		`{}`,
	} {
		flags.SetFlagPayload(feature.FlagRiskEnforcementMaxContentBytes, orgID, []byte(payload))
		limit, source := EnforcementMaxContentBytes(t.Context(), logger, queries, flags, orgID, projectID)
		require.Equal(t, enforcereply.DefaultMaxContentBytes, limit)
		require.Equal(t, "default", source)
	}
}
