package roledistribution_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	roledistributionv1 "github.com/speakeasy-api/gram/infra/gen/gram/role_distribution/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	relay "github.com/speakeasy-api/gram/server/internal/background/activities/publish_outbox"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/roledistribution"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// pipelinePublisher replaces only the external broker. Source SQL, outbox
// encoding, relay claiming/settlement, decoding, handler and processor are real.
// Captured messages are passed to the handler manually, not via a live broker
// or streams wiring. This does not prove IAM, redelivery, DLQ or latency behavior.
type pipelinePublisher struct {
	mu       sync.Mutex
	messages [][]byte
}

func (p *pipelinePublisher) Publish(_ context.Context, topic string, data []byte, _ map[string]string) gcp.PublishResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	if topic == "gram.role_distribution.v1.RoleDistributionSetupRequestedV1" {
		p.messages = append(p.messages, append([]byte(nil), data...))
	}
	return gcp.NewSuccessPublishResult()
}
func (*pipelinePublisher) Stop(context.Context) error { return nil }
func (p *pipelinePublisher) captured() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][]byte(nil), p.messages...)
}

func TestRoleDistributionPipeline_AdditiveIdempotency(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	fixtures := testrepo.New(f.db)
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "pipeline-message"}))
	count, err := fixtures.PipelineCountPlugins(ctx, f.project)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	plugin, err := fixtures.PipelineEngineeringPlugin(ctx, f.project)
	require.NoError(t, err)
	principal, err := fixtures.PipelinePluginPrincipal(ctx, plugin)
	require.NoError(t, err)
	require.Equal(t, f.roleURN, principal)
	err = fixtures.PipelineRenamePlugin(ctx, testrepo.PipelineRenamePluginParams{ID: plugin, ProjectID: f.project})
	require.NoError(t, err)
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "duplicate"}))
	edited, err := pluginsrepo.New(f.db).GetPlugin(ctx, pluginsrepo.GetPluginParams{ID: plugin, OrganizationID: f.event.GetOrganizationId(), ProjectID: f.project})
	require.NoError(t, err)
	require.Equal(t, "Administrator edit", edited.Name)
	count, err = fixtures.PipelineCountPluginAssignments(ctx, plugin)
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "duplicate delivery must not duplicate assignments")
	count, err = fixtures.PipelineCountPlugins(ctx, f.project)
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "duplicate delivery must reuse the exact-slug plugin")
	require.Equal(t, "engineering", edited.Slug)
}

func TestRoleDistributionPipeline_PreservesExactSlugPlugin(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	q := pluginsrepo.New(f.db)
	curated, err := q.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: f.event.GetOrganizationId(), ProjectID: f.project,
		Name: "Curated tools", Slug: "engineering", Description: conv.ToPGText("Administrator description"),
	})
	require.NoError(t, err)
	other, err := q.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: f.event.GetOrganizationId(), ProjectID: f.project,
		Name: "Engineering", Slug: "other-tools", Description: pgtype.Text{},
	})
	require.NoError(t, err)
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "existing-slug"}))
	got, err := q.GetPlugin(ctx, pluginsrepo.GetPluginParams{ID: curated.ID, OrganizationID: f.event.GetOrganizationId(), ProjectID: f.project})
	require.NoError(t, err)
	require.Equal(t, curated.Name, got.Name)
	require.Equal(t, curated.Slug, got.Slug)
	require.Equal(t, curated.Description, got.Description)
	fixtures := testrepo.New(f.db)
	principal, err := fixtures.PipelinePluginPrincipal(ctx, curated.ID)
	require.NoError(t, err)
	require.Equal(t, f.roleURN, principal)
	count, err := fixtures.PipelineCountPluginAssignments(ctx, other.ID)
	require.NoError(t, err)
	require.Zero(t, count, "a matching name must not override exact slug selection")
	count, err = fixtures.PipelineCountPlugins(ctx, f.project)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}

func TestRoleDistributionPipeline_SourceRollback(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	fixtures := testrepo.New(f.db)
	before, err := fixtures.PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	tx, err := f.db.Begin(ctx) //nolint:glint // notestingrawsql: The source transaction boundary is the behavior under test.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	now := conv.ToPGTimestamptz(time.Now())
	role, err := accessrepo.New(tx).CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{
		OrganizationID: f.event.GetOrganizationId(), WorkosSlug: "rolled-back", WorkosName: "Rolled back",
		WorkosCreatedAt: now, WorkosUpdatedAt: now,
	})
	require.NoError(t, err)
	pending, err := testrepo.New(tx).PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	require.Equal(t, before+1, pending)
	require.NoError(t, tx.Rollback(ctx))
	after, err := fixtures.PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	require.Equal(t, before, after, "source rollback must roll back its event")
	count, err := fixtures.PipelineCountOrganizationRole(ctx, role.ID)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRoleDistributionPipeline_Disabled(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	fixtures := testrepo.New(f.db)
	require.NoError(t, fixtures.SourceDisableRoleDistributionSetup(ctx, f.event.GetOrganizationId()))
	require.NoError(t, f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "pipeline-message"}))
	count, err := fixtures.PipelineCountPlugins(ctx, f.project)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRoleDistributionPipeline_PublicationRollback(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := newPipelineFixture(t)
	fixtures := testrepo.New(f.db)
	_, err := pluginsrepo.New(f.db).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{ProjectID: f.project, InstallationID: 12345, RepoOwner: "test-org", RepoName: "test-plugins", MarketplaceToken: pgtype.Text{String: "test-token", Valid: true}, PublishedMcpFingerprints: []byte(`{}`)})
	require.NoError(t, err)
	require.NoError(t, fixtures.PipelineInstallPublicationFailure(ctx))
	err = f.handler.HandleRoleDistributionSetupRequested(ctx, f.event, gcp.MessageMetadata{ID: "pipeline-message"})
	require.ErrorContains(t, err, "injected publication failure")
	count, err := fixtures.PipelineCountPlugins(ctx, f.project)
	require.NoError(t, err)
	require.Zero(t, count)
}

type pipelineFixture struct {
	db      *pgxpool.Pool
	project uuid.UUID
	roleURN string
	event   *roledistributionv1.RoleDistributionSetupRequestedV1
	handler *roledistribution.Handler
}

func newPipelineFixture(t *testing.T) pipelineFixture {
	t.Helper()
	env, cleanup, err := testenv.Launch(t.Context(), testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	// Use a disposable clone, never the worktree/user database.
	ctx := t.Context()
	db, err := env.CloneTestDatabase(t, "pipeline")
	require.NoError(t, err)
	org := "org-pipeline"
	project := uuid.New()
	fixtures := testrepo.New(db)
	err = fixtures.CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{ID: org, Name: "Pipeline", Slug: "pipeline", GramAccountType: "free", Whitelisted: true, FreeTrialStartedAt: conv.ToPGTimestamptz(time.Now()), FreeTrialEndsAt: conv.ToPGTimestamptz(time.Now().Add(14 * 24 * time.Hour))})
	require.NoError(t, err)
	_, err = fixtures.CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: project, OrganizationID: org, Name: "Pipeline", Slug: "pipeline"})
	require.NoError(t, err)
	err = fixtures.PipelineEnableRoleDistribution(ctx, org)
	require.NoError(t, err)
	pub := &pipelinePublisher{}
	drain := relay.New(testenv.NewLogger(t), testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), db, pub)
	tx, err := db.Begin(ctx) //nolint:glint // notestingrawsql: Real transaction boundary proves the relay cannot observe uncommitted source writes.
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	now := conv.ToPGTimestamptz(time.Now())
	role, err := accessrepo.New(tx).CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{OrganizationID: org, WorkosSlug: "engineering", WorkosName: "Engineering", WorkosCreatedAt: now, WorkosUpdatedAt: now})
	require.NoError(t, err)
	localOutbox, err := testrepo.New(tx).PipelineCountRoleDistributionOutbox(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, localOutbox)
	result, err := drain.Drain(ctx)
	require.NoError(t, err)
	require.Zero(t, result.Published, "relay cannot see uncommitted source writes")
	require.Empty(t, pub.captured())
	visibleRoles, err := fixtures.PipelineCountOrganizationRole(ctx, role.ID)
	require.NoError(t, err)
	require.Zero(t, visibleRoles)
	require.NoError(t, tx.Commit(ctx))
	result, err = drain.Drain(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Published)
	messages := pub.captured()
	require.Len(t, messages, 1)
	event := &roledistributionv1.RoleDistributionSetupRequestedV1{}
	require.NoError(t, proto.Unmarshal(messages[0], event))
	require.Equal(t, org, event.GetOrganizationId())
	require.Equal(t, role.RoleUrn, event.GetRoleUrn())
	publication := plugins.PublicationRequests{Enabled: true}
	guard := admission.NewGuard(new(feature.InMemory), admission.NewReportMetrics(testenv.NewMeterProvider(t), testenv.NewLogger(t)))
	handler := roledistribution.NewHandler(testenv.NewLogger(t), roledistribution.Processors{Setup: func(ctx context.Context, roleURN, organization string) (bool, error) {
		processed, err := roledistribution.ProcessRoleDistributionSetup(ctx, db, publication, guard, roleURN, organization)
		if err != nil {
			return false, fmt.Errorf("process pipeline setup: %w", err)
		}
		return processed, nil
	}})
	return pipelineFixture{db: db, project: project, roleURN: role.RoleUrn, event: event, handler: handler}
}
