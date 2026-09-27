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
	fx := seedFlagFixture(t)
	orgID, projectID, queries := fx.orgID, fx.projectID, fx.queries
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagRiskLLMAnalyzer, orgID, true)
	provider := &countingProvider{Provider: flags}
	logger := testenv.NewLogger(t)

	ctx := WithRequestMemo(t.Context())
	for range 3 {
		on, slug := ProjectFlagState(ctx, logger, queries, provider, orgID, projectID, feature.FlagRiskLLMAnalyzer)
		require.True(t, on)
		require.Equal(t, orgID, slug)
	}
	require.EqualValues(t, 1, provider.calls.Load())
	require.False(t, ProjectFlagEnabled(ctx, logger, queries, provider, orgID, projectID, feature.FlagRiskEnforcementPubsub))
	require.False(t, ProjectFlagEnabled(ctx, logger, queries, provider, orgID, projectID, feature.FlagRiskEnforcementPubsub))
	require.EqualValues(t, 2, provider.calls.Load())

	require.True(t, ProjectFlagEnabled(t.Context(), logger, queries, provider, orgID, projectID, feature.FlagRiskLLMAnalyzer))
	require.True(t, ProjectFlagEnabled(t.Context(), logger, queries, provider, orgID, projectID, feature.FlagRiskLLMAnalyzer))
	require.EqualValues(t, 4, provider.calls.Load())

	// Concurrent scans sharing one memo still resolve each flag once.
	provider.calls.Store(0)
	ctx = WithRequestMemo(t.Context())
	var group sync.WaitGroup
	for range 8 {
		for _, flag := range []feature.Flag{feature.FlagRiskLLMAnalyzer, feature.FlagRiskEnforcementPubsub} {
			group.Go(func() { _ = ProjectFlagEnabled(ctx, logger, queries, provider, orgID, projectID, flag) })
		}
	}
	group.Wait()
	require.EqualValues(t, 2, provider.calls.Load())

	// A failed lookup is not remembered; the next scan retries it.
	failing := &countingProvider{Provider: failingProvider{}}
	ctx = WithRequestMemo(t.Context())
	require.False(t, ProjectFlagEnabled(ctx, logger, queries, failing, orgID, projectID, feature.FlagRiskLLMAnalyzer))
	require.False(t, ProjectFlagEnabled(ctx, logger, queries, failing, orgID, projectID, feature.FlagRiskLLMAnalyzer))
	require.EqualValues(t, 2, failing.calls.Load())
}

type failingProvider struct{ feature.Provider }

func (failingProvider) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, errors.New("flag service unavailable")
}

// countingVariantProvider counts variant and boolean reads separately and
// remembers the groups the last variant read carried.
type countingVariantProvider struct {
	*feature.InMemory
	variantCalls atomic.Int32
	boolCalls    atomic.Int32
	mu           sync.Mutex
	lastGroups   map[string]string
}

func (p *countingVariantProvider) FlagVariant(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (feature.Variant, error) {
	p.variantCalls.Add(1)
	p.mu.Lock()
	p.lastGroups = groups
	p.mu.Unlock()
	variant, err := p.InMemory.FlagVariant(ctx, flag, distinctID, groups)
	if err != nil {
		return "", fmt.Errorf("counting variant provider: %w", err)
	}
	return variant, nil
}

func (p *countingVariantProvider) IsFlagEnabled(ctx context.Context, flag feature.Flag, distinctID string, groups map[string]string) (bool, error) {
	p.boolCalls.Add(1)
	on, err := p.InMemory.IsFlagEnabled(ctx, flag, distinctID, groups)
	if err != nil {
		return false, fmt.Errorf("counting variant provider: %w", err)
	}
	return on, nil
}

func (p *countingVariantProvider) groups() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastGroups
}

// flagFixture is one organization with one project (slug "example") in a
// fresh database clone, the rows every flag resolution needs for its groups.
type flagFixture struct {
	orgID     string
	projectID uuid.UUID
	queries   *repo.Queries
}

func seedFlagFixture(t *testing.T) flagFixture {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, "policyflagstest")
	require.NoError(t, err)
	orgID := "org_" + uuid.NewString()
	_, err = orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{
		ID: orgID, Name: "Flags Example", Slug: orgID, WorkosID: pgtype.Text{String: orgID, Valid: true}, Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	project, err := projectsrepo.New(db).CreateProject(t.Context(), projectsrepo.CreateProjectParams{Name: "Flags Example", Slug: "example", OrganizationID: orgID})
	require.NoError(t, err)
	return flagFixture{orgID: orgID, projectID: project.ID, queries: repo.New(db)}
}

func TestProjectFlagModeResolvesOncePerRequestWithGroups(t *testing.T) {
	t.Parallel()
	fx := seedFlagFixture(t)
	flags := &feature.InMemory{}
	flags.SetFlagVariant(feature.FlagRiskLLMAnalyzer, fx.orgID, feature.VariantRiskLLMShadow)
	provider := &countingVariantProvider{InMemory: flags}
	logger := testenv.NewLogger(t)

	ctx := WithRequestMemo(t.Context())
	for range 3 {
		mode, slug := ProjectFlagMode(ctx, logger, fx.queries, provider, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
		require.Equal(t, feature.VariantRiskLLMShadow, mode)
		require.Equal(t, fx.orgID, slug)
	}
	require.EqualValues(t, 1, provider.variantCalls.Load(), "one variant read per request")
	require.EqualValues(t, 0, provider.boolCalls.Load(), "an explicit variant skips the boolean read")
	require.Equal(t, map[string]string{"organization": fx.orgID, "slug": fx.orgID + "/example"}, provider.groups())

	// The mode memo is independent of the boolean memo for the same key.
	on, _ := ProjectFlagState(ctx, logger, fx.queries, provider, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	require.False(t, on, "no boolean entry is set for the key")
	require.EqualValues(t, 1, provider.boolCalls.Load())

	// Without a memo every call resolves.
	_, _ = ProjectFlagMode(t.Context(), logger, fx.queries, provider, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	_, _ = ProjectFlagMode(t.Context(), logger, fx.queries, provider, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	require.EqualValues(t, 3, provider.variantCalls.Load())

	// Concurrent scans sharing one memo still resolve once.
	provider.variantCalls.Store(0)
	ctx = WithRequestMemo(t.Context())
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			_, _ = ProjectFlagMode(ctx, logger, fx.queries, provider, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
		})
	}
	group.Wait()
	require.EqualValues(t, 1, provider.variantCalls.Load())
}

func TestProjectFlagModeBooleanTrueWithoutVariantReadsLLM(t *testing.T) {
	t.Parallel()
	fx := seedFlagFixture(t)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagRiskLLMAnalyzer, fx.orgID, true)
	provider := &countingVariantProvider{InMemory: flags}
	logger := testenv.NewLogger(t)

	ctx := WithRequestMemo(t.Context())
	mode, slug := ProjectFlagMode(ctx, logger, fx.queries, provider, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	require.Equal(t, feature.VariantRiskLLMLLM, mode, "a boolean-only key keeps the org on the model")
	require.Equal(t, fx.orgID, slug)
	require.EqualValues(t, 1, provider.variantCalls.Load())
	require.EqualValues(t, 1, provider.boolCalls.Load(), "the boolean read happens only when no variant resolved")

	_, _ = ProjectFlagMode(ctx, logger, fx.queries, provider, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	require.EqualValues(t, 1, provider.boolCalls.Load(), "the boolean read is memoized with the mode")
}

func TestProjectFlagModeExplicitOffBeatsBooleanTrue(t *testing.T) {
	t.Parallel()
	fx := seedFlagFixture(t)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagRiskLLMAnalyzer, fx.orgID, true)
	flags.SetFlagVariant(feature.FlagRiskLLMAnalyzer, fx.orgID, feature.VariantRiskLLMOff)
	provider := &countingVariantProvider{InMemory: flags}

	mode, slug := ProjectFlagMode(t.Context(), testenv.NewLogger(t), fx.queries, provider, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	require.Equal(t, feature.VariantRiskLLMOff, mode)
	require.Equal(t, fx.orgID, slug)
	require.EqualValues(t, 0, provider.boolCalls.Load())
}

func TestProjectFlagModeUnknownOrMissingReadsOff(t *testing.T) {
	t.Parallel()
	fx := seedFlagFixture(t)
	logger := testenv.NewLogger(t)

	mode, slug := ProjectFlagMode(t.Context(), logger, fx.queries, &feature.InMemory{}, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	require.Equal(t, feature.VariantRiskLLMOff, mode, "an absent flag is off")
	require.Equal(t, fx.orgID, slug, "the slug still resolves for an absent flag")

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagRiskLLMAnalyzer, fx.orgID, true)
	flags.SetFlagVariant(feature.FlagRiskLLMAnalyzer, fx.orgID, feature.Variant("experimental"))
	mode, _ = ProjectFlagMode(t.Context(), logger, fx.queries, flags, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	require.Equal(t, feature.VariantRiskLLMOff, mode, "an unrecognized variant is off even with the boolean on")

	mode, slug = ProjectFlagMode(t.Context(), logger, fx.queries, nil, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	require.Equal(t, feature.VariantRiskLLMOff, mode, "no provider is off")
	require.Empty(t, slug)
}

func TestProjectFlagModeFailureReadsOffAndIsNotMemoized(t *testing.T) {
	t.Parallel()
	fx := seedFlagFixture(t)
	logger := testenv.NewLogger(t)

	// A variant read that errors reads as off and is retried by the next scan.
	variantFailing := &countingVariantProvider{InMemory: &feature.InMemory{}}
	failing := failingVariantProvider{countingVariantProvider: variantFailing}
	ctx := WithRequestMemo(t.Context())
	mode, slug := ProjectFlagMode(ctx, logger, fx.queries, failing, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	require.Equal(t, feature.VariantRiskLLMOff, mode)
	require.Empty(t, slug)
	mode, _ = ProjectFlagMode(ctx, logger, fx.queries, failing, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	require.Equal(t, feature.VariantRiskLLMOff, mode)
	require.EqualValues(t, 2, failing.variantCalls.Load())

	// A boolean read that errors after an empty variant reads as off too.
	boolFailing := &countingVariantProvider{InMemory: &feature.InMemory{}}
	ctx = WithRequestMemo(t.Context())
	mode, slug = ProjectFlagMode(ctx, logger, fx.queries, failingBoolVariantProvider{boolFailing}, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	require.Equal(t, feature.VariantRiskLLMOff, mode)
	require.Empty(t, slug)
	_, _ = ProjectFlagMode(ctx, logger, fx.queries, failingBoolVariantProvider{boolFailing}, fx.orgID, fx.projectID, feature.FlagRiskLLMAnalyzer)
	require.EqualValues(t, 2, boolFailing.variantCalls.Load(), "a failed lookup is retried by the next scan")

	// A project with no flag groups reads as off.
	mode, slug = ProjectFlagMode(t.Context(), logger, fx.queries, &feature.InMemory{}, fx.orgID, uuid.New(), feature.FlagRiskLLMAnalyzer)
	require.Equal(t, feature.VariantRiskLLMOff, mode)
	require.Empty(t, slug)
}

// failingVariantProvider errors on every variant read and counts them.
type failingVariantProvider struct {
	*countingVariantProvider
}

func (p failingVariantProvider) FlagVariant(context.Context, feature.Flag, string, map[string]string) (feature.Variant, error) {
	p.variantCalls.Add(1)
	return "", errors.New("flag service unavailable")
}

// failingBoolVariantProvider resolves no variant and errors on the boolean
// read that follows.
type failingBoolVariantProvider struct {
	*countingVariantProvider
}

func (p failingBoolVariantProvider) IsFlagEnabled(context.Context, feature.Flag, string, map[string]string) (bool, error) {
	return false, errors.New("flag service unavailable")
}
