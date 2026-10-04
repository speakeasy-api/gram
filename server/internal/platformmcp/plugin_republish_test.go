package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	publicationv1 "github.com/speakeasy-api/gram/infra/gen/gram/plugins/v1"
	"github.com/speakeasy-api/gram/server/internal/conv"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	plugindelivery "github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

type pluginPublishSignal struct {
	projectID uuid.UUID
	userID    string
}

type recordingPluginPublishSignaler struct {
	mu      sync.Mutex
	signals []pluginPublishSignal
}

func (r *recordingPluginPublishSignaler) SignalPluginPublish(_ context.Context, projectID uuid.UUID, createdByUserID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.signals = append(r.signals, pluginPublishSignal{projectID: projectID, userID: createdByUserID})
	return nil
}

func (r *recordingPluginPublishSignaler) recorded() []pluginPublishSignal {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]pluginPublishSignal{}, r.signals...)
}

func staleEvidence(slug string, fresh *bool) stubPluginPublicationEvidence {
	return stubPluginPublicationEvidence{items: []plugindelivery.PublicationEvidence{{
		PluginSlug: slug, NotConfigured: false, Fresh: fresh,
		Packages: []plugindelivery.PublicationPackageAddress{{ServerName: "MCP", MCPURL: "https://mcp.example.test/mcp/first"}},
	}}}
}

func requireRepublishRefusal(t *testing.T, err error, code string) *PluginRepublishError {
	t.Helper()
	refusal, ok := errors.AsType[*PluginRepublishError](err)
	require.True(t, ok, "expected a republish refusal, got %v", err)
	require.Equal(t, code, refusal.Code)
	return refusal
}

func requireNoRepublishReceipt(t *testing.T, ctx context.Context, conn *pgxpool.Pool, principal Principal, project ResolvedProject, key string) {
	t.Helper()
	_, err := platformrepo.New(conn).GetPlatformMCPOperationReceipt(ctx, platformrepo.GetPlatformMCPOperationReceiptParams{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, Operation: operationRepublishPlugin,
		IdempotencyKey: key, UserID: conv.ToPGText(principal.UserID), SubjectUrn: userSubjectURN(principal.UserID),
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestRepublishPluginRequiresConfirmation(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_republish_confirmation")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Support Tools", "support-tools")
	signaler := &recordingPluginPublishSignaler{}
	service := testPluginTargets(conn).WithPublicationEvidence(staleEvidence("support-tools", new(false))).
		WithRepublish(plugindelivery.PublicationRequests{Enabled: false}, signaler, testOperationBudget())

	_, err = service.RepublishPlugin(ctx, principal, RepublishPluginInput{ProjectID: project.ID.String(), Plugin: "support-tools", IdempotencyKey: "unconfirmed"})
	refusal := requireRepublishRefusal(t, err, "confirmation_required")
	require.Contains(t, refusal.Message, "every plugin in the project")
	require.Empty(t, signaler.recorded())
	requireNoRepublishReceipt(t, ctx, conn, principal, project, "unconfirmed")
}

func TestRepublishPluginRefusesWhenPublishingIsNotConfigured(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_republish_not_configured")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Support Tools", "support-tools")
	signaler := &recordingPluginPublishSignaler{}
	evidence := stubPluginPublicationEvidence{items: []plugindelivery.PublicationEvidence{{PluginSlug: "support-tools", NotConfigured: true}}}
	service := testPluginTargets(conn).WithPublicationEvidence(evidence).
		WithRepublish(plugindelivery.PublicationRequests{Enabled: true}, signaler, testOperationBudget())

	_, err = service.RepublishPlugin(ctx, principal, RepublishPluginInput{ProjectID: project.ID.String(), Plugin: "support-tools", Confirmed: true, IdempotencyKey: "not-configured"})
	refusal := requireRepublishRefusal(t, err, "not_configured")
	require.ErrorIs(t, err, ErrPluginRepublishNotConfigured)
	require.Contains(t, refusal.Message, "AICP dashboard")
	require.Empty(t, signaler.recorded())
	requireNoRepublishReceipt(t, ctx, conn, principal, project, "not-configured")
}

func TestRepublishPluginReportsAlreadyCurrentWithoutRequestingAPublish(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_republish_already_current")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	plugin := seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Support Tools", "support-tools")
	seedMarketplaceConnection(t, ctx, conn, project.ID)
	signaler := &recordingPluginPublishSignaler{}
	service := testPluginTargets(conn).WithPublicationEvidence(staleEvidence("support-tools", new(true))).
		WithRepublish(plugindelivery.PublicationRequests{Enabled: true}, signaler, testOperationBudget())
	baseline, err := testrepo.New(conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)

	got, err := service.RepublishPlugin(ctx, principal, RepublishPluginInput{ProjectID: project.ID.String(), Plugin: plugin.ID.String(), Confirmed: true, IdempotencyKey: "already-current"})
	require.NoError(t, err)
	require.Equal(t, PluginRepublishAlreadyCurrent, got.Outcome)
	require.Equal(t, RepublishedPlugin{ID: plugin.ID.String(), Name: "Support Tools", Slug: "support-tools", IsDefault: false}, got.Plugin)
	require.NotNil(t, got.PublicationEvidence)
	require.Equal(t, new(true), got.PublicationEvidence.Fresh)
	require.Equal(t, pluginAlreadyCurrentNote, got.Note)
	require.Empty(t, signaler.recorded())
	count, err := testrepo.New(conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, baseline, count, "a current package requests nothing")
}

func TestRepublishPluginSignalsTheProjectWhenEmissionIsDisabled(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_republish_signal")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Support Tools", "support-tools")
	signaler := &recordingPluginPublishSignaler{}
	service := testPluginTargets(conn).WithPublicationEvidence(staleEvidence("support-tools", new(false))).
		WithRepublish(plugindelivery.PublicationRequests{Enabled: false}, signaler, testOperationBudget())
	input := RepublishPluginInput{ProjectID: project.ID.String(), Plugin: "Support Tools", Confirmed: true, IdempotencyKey: "signal"}

	got, err := service.RepublishPlugin(ctx, principal, input)
	require.NoError(t, err)
	require.Equal(t, PluginRepublishEnqueued, got.Outcome)
	require.Equal(t, project.ID.String(), got.ProjectID)
	require.False(t, got.Receipt.Replayed)
	require.Equal(t, pluginRepublishNote, got.Note)
	require.NotNil(t, got.PublicationEvidence)
	require.Equal(t, new(false), got.PublicationEvidence.Fresh, "the post-call read reports the package as it stands, not as requested")
	require.Equal(t, []pluginPublishSignal{{projectID: project.ID, userID: principal.UserID}}, signaler.recorded())

	replayed, err := service.RepublishPlugin(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, got.Receipt.ID, replayed.Receipt.ID)
	require.Equal(t, PluginRepublishEnqueued, replayed.Outcome)
	require.Len(t, signaler.recorded(), 2, "a replay re-signals so a retry after a failed signal recovers; the publish debounce collapses it")
}

func TestRepublishPluginEnqueuesADurableRequestForAConnectedMarketplace(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_republish_outbox")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Support Tools", "support-tools")
	seedMarketplaceConnection(t, ctx, conn, project.ID)
	signaler := &recordingPluginPublishSignaler{}
	service := testPluginTargets(conn).WithPublicationEvidence(staleEvidence("support-tools", nil)).
		WithRepublish(plugindelivery.PublicationRequests{Enabled: true}, signaler, testOperationBudget())
	baseline, err := testrepo.New(conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)

	got, err := service.RepublishPlugin(ctx, principal, RepublishPluginInput{ProjectID: project.ID.String(), Plugin: "support-tools", Confirmed: true, IdempotencyKey: "durable"})
	require.NoError(t, err)
	require.Equal(t, PluginRepublishEnqueued, got.Outcome)
	require.Empty(t, signaler.recorded(), "a durable request already covers the publish")

	count, err := testrepo.New(conn).CountPublishOutboxRows(ctx)
	require.NoError(t, err)
	require.Equal(t, baseline+1, count)
	rows, err := testrepo.New(conn).ListPublishOutboxRows(ctx)
	require.NoError(t, err)
	requested := 0
	for _, row := range rows {
		if row.Topic != "gram.plugins.v1.PublicationRequested" {
			continue
		}
		requested++
		var published publicationv1.PublicationRequested
		require.NoError(t, proto.Unmarshal(row.Message, &published))
		require.Equal(t, principal.OrganizationID, published.GetOrganizationId())
		require.Equal(t, project.ID.String(), published.GetProjectId())
		require.Equal(t, principal.UserID, published.GetCreatedByUserId())
	}
	require.Equal(t, 1, requested)
}

func TestRepublishPluginRefusesAnInexactTargetRatherThanFallingBackToDefault(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_republish_targets")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	_, otherProject := seedRegistrationLifecycle(t, ctx, conn)
	_, err = pluginsrepo.New(conn).CreateDefaultPlugin(ctx, pluginsrepo.CreateDefaultPluginParams{OrganizationID: principal.OrganizationID, ProjectID: project.ID})
	require.NoError(t, err)
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared", "shared-one")
	seedPlugin(t, ctx, conn, principal.OrganizationID, project.ID, "Shared", "shared-two")
	signaler := &recordingPluginPublishSignaler{}
	service := testPluginTargets(conn).WithPublicationEvidence(staleEvidence("shared-one", new(false))).
		WithRepublish(plugindelivery.PublicationRequests{Enabled: false}, signaler, testOperationBudget())

	_, err = service.RepublishPlugin(ctx, principal, RepublishPluginInput{ProjectID: project.ID.String(), Plugin: "marketing", Confirmed: true, IdempotencyKey: "missing"})
	require.ErrorIs(t, err, ErrPluginNotFound)
	_, err = service.RepublishPlugin(ctx, principal, RepublishPluginInput{ProjectID: project.ID.String(), Plugin: "Shared", Confirmed: true, IdempotencyKey: "ambiguous"})
	require.ErrorIs(t, err, ErrPluginAmbiguous)
	_, err = service.RepublishPlugin(ctx, principal, RepublishPluginInput{ProjectID: otherProject.ID.String(), Plugin: "shared-one", Confirmed: true, IdempotencyKey: "foreign"})
	require.ErrorIs(t, err, ErrPluginProjectNotFound)
	require.Empty(t, signaler.recorded())

	for _, test := range []struct {
		err  error
		code string
	}{
		{ErrPluginNotFound, "not_found"},
		{ErrPluginAmbiguous, "ambiguous_target"},
		{ErrPluginProjectNotFound, "not_found"},
	} {
		result, ok := republishPluginToolResult(test.err)
		require.True(t, ok)
		require.True(t, result.IsError)
		text, ok := result.Content[0].(*mcp.TextContent)
		require.True(t, ok)
		var refusal pluginRefusalResult
		require.NoError(t, json.Unmarshal([]byte(text.Text), &refusal))
		require.Equal(t, test.code, refusal.Code)
	}
}

func TestComposedRepublishPluginToolDeclaresAnExternalAdminIdempotentWrite(t *testing.T) {
	t.Parallel()

	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_republish_tool")
	require.NoError(t, err)
	plugins := testPluginTargets(conn).WithPublicationEvidence(stubPluginPublicationEvidence{}).
		WithRepublish(plugindelivery.PublicationRequests{Enabled: true}, nil, testOperationBudget())
	require.True(t, plugins.republishValid())

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, plugins, nil, CatalogDescriptor{})
	requireRepublishPluginDeclaration(t, registrar)
	require.Equal(t, republishPluginDescription, descriptorByName(t, registrar, operationRepublishPlugin).Description)
}

// Plugin reads composed without a publish path keep republish_plugin listed
// with the live contract, so a deployment flip changes its answer rather than
// whether it exists.
func TestPluginReadsWithoutAPublishPathKeepRepublishPluginDeclared(t *testing.T) {
	t.Parallel()

	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_republish_tool_unavailable")
	require.NoError(t, err)
	plugins := testPluginTargets(conn)
	require.True(t, plugins.valid())
	require.False(t, plugins.republishValid())

	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, nil, nil, plugins, nil, CatalogDescriptor{})
	requireRepublishPluginDeclaration(t, registrar)
}

func seedMarketplaceConnection(t *testing.T, ctx context.Context, conn *pgxpool.Pool, projectID uuid.UUID) {
	t.Helper()

	_, err := pluginsrepo.New(conn).UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{
		ProjectID: projectID, InstallationID: 1, RepoOwner: "example-owner", RepoName: "example-marketplace",
		MarketplaceToken: pgtype.Text{}, PublishedMcpFingerprints: nil, PublishedHooksVersion: pgtype.Text{}, PublishedHooksConfig: nil,
	})
	require.NoError(t, err)
}
