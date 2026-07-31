package wraptoolsets

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/cmd/tools/migrations/wraptoolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestRun_DryRunMakesNoWrites(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-dry-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	metadata := tn.attachMetadata(t, toolset.ID)
	collection := tn.newCollection(t)
	tn.attachToolsetToCollection(t, collection, toolset.ID)

	report := runWrap(t, tn, dryRunOptions())

	require.Len(t, report.Rows, 1)
	row := report.Rows[0]
	require.Equal(t, OutcomeWouldCreate, row.Outcome)
	require.Equal(t, toolset.ID, row.ToolsetID)
	require.Equal(t, tn.projectID, row.ProjectID)
	require.EqualValues(t, 1, row.MetadataMoved)
	require.EqualValues(t, 1, row.AttachmentsMoved)

	q := tn.queries()
	servers, err := q.CountMcpServersInProject(ctx, tn.projectID)
	require.NoError(t, err)
	require.Zero(t, servers)
	endpoints, err := q.CountMcpEndpointsInProject(ctx, tn.projectID)
	require.NoError(t, err)
	require.Zero(t, endpoints)

	metadataRow, err := q.GetMetadataRow(ctx, repo.GetMetadataRowParams{ID: metadata.ID, ProjectID: tn.projectID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: toolset.ID, Valid: true}, metadataRow.ToolsetID)
	require.False(t, metadataRow.McpServerID.Valid)
}

func TestRun_ApplyCreatesWrapperAndEndpoint(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	envID := tn.newEnvironment(t, "prod")
	mcpSlug := "wrap-apply-" + uuid.NewString()[:8]
	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        mcpSlug,
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "prod",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})

	report := runWrap(t, tn, applyOptions())

	require.Len(t, report.Rows, 1)
	row := report.Rows[0]
	require.Equal(t, OutcomeCreated, row.Outcome)
	require.NotNil(t, row.McpServerID)
	require.NotNil(t, row.McpEndpointID)

	// Deterministic ids derived independently of the implementation helpers.
	expectedServerID := uuid.NewSHA1(idNamespace, []byte("wraptoolsets:v1:server:"+toolset.ID.String()))
	expectedEndpointID := uuid.NewSHA1(idNamespace, []byte("wraptoolsets:v1:endpoint:"+toolset.ID.String()))
	require.Equal(t, expectedServerID, *row.McpServerID)
	require.Equal(t, expectedEndpointID, *row.McpEndpointID)

	q := tn.queries()
	server, err := q.GetMcpServerRow(ctx, expectedServerID)
	require.NoError(t, err)
	require.Equal(t, tn.projectID, server.ProjectID)
	require.Equal(t, conv.ToPGText(toolset.Name), server.Name)
	compactID := strings.ReplaceAll(toolset.ID.String(), "-", "")
	require.Equal(t, conv.ToPGText(toolset.Slug+"-"+compactID[:8]), server.Slug)
	require.Equal(t, uuid.NullUUID{UUID: envID, Valid: true}, server.EnvironmentID)
	require.Equal(t, uuid.NullUUID{UUID: toolset.ID, Valid: true}, server.ToolsetID)
	require.False(t, server.UserSessionIssuerID.Valid)
	require.False(t, server.RemoteMcpServerID.Valid)
	require.False(t, server.TunneledMcpServerID.Valid)
	require.False(t, server.ToolVariationsGroupID.Valid)
	require.Equal(t, "private", server.Visibility)
	require.False(t, server.Deleted)

	endpoint, err := q.GetMcpEndpointRow(ctx, expectedEndpointID)
	require.NoError(t, err)
	require.Equal(t, tn.projectID, endpoint.ProjectID)
	require.Equal(t, expectedServerID, endpoint.McpServerID)
	require.Equal(t, mcpSlug, endpoint.Slug)
	require.False(t, endpoint.CustomDomainID.Valid)
	require.False(t, endpoint.IsDomainRoot.Valid)
	require.False(t, endpoint.Deleted)
}

func TestRun_VisibilityDisabledWinsOverPublic(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)

	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-vis-" + uuid.NewString()[:8],
		mcpEnabled:     false,
		mcpIsPublic:    true,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})

	report := runWrap(t, tn, applyOptions())

	require.Len(t, report.Rows, 1)
	require.Equal(t, OutcomeCreated, report.Rows[0].Outcome)

	server, err := tn.queries().GetMcpServerRow(t.Context(), deriveServerID(toolset.ID))
	require.NoError(t, err)
	require.Equal(t, "disabled", server.Visibility)
}

func TestRun_VisibilityPublic(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)

	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-pub-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    true,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})

	report := runWrap(t, tn, applyOptions())

	require.Len(t, report.Rows, 1)
	require.Equal(t, OutcomeCreated, report.Rows[0].Outcome)

	server, err := tn.queries().GetMcpServerRow(t.Context(), deriveServerID(toolset.ID))
	require.NoError(t, err)
	require.Equal(t, "public", server.Visibility)
}

func TestRun_RerunReportsAlreadyCompleteWithZeroWrites(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-rerun-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	metadata := tn.attachMetadata(t, toolset.ID)
	collection := tn.newCollection(t)
	tn.attachToolsetToCollection(t, collection, toolset.ID)

	first := runWrap(t, tn, applyOptions())
	require.Len(t, first.Rows, 1)
	require.Equal(t, OutcomeCreated, first.Rows[0].Outcome)

	q := tn.queries()
	serverBefore, err := q.GetMcpServerRow(ctx, deriveServerID(toolset.ID))
	require.NoError(t, err)
	endpointBefore, err := q.GetMcpEndpointRow(ctx, deriveEndpointID(toolset.ID))
	require.NoError(t, err)
	metadataBefore, err := q.GetMetadataRow(ctx, repo.GetMetadataRowParams{ID: metadata.ID, ProjectID: tn.projectID})
	require.NoError(t, err)
	attachmentsBefore, err := q.ListCollectionAttachmentRows(ctx, collection)
	require.NoError(t, err)

	second := runWrap(t, tn, applyOptions())
	require.Len(t, second.Rows, 1)
	row := second.Rows[0]
	require.Equal(t, OutcomeAlreadyComplete, row.Outcome)
	require.Zero(t, row.MetadataMoved)
	require.Zero(t, row.AttachmentsMoved)

	serverAfter, err := q.GetMcpServerRow(ctx, deriveServerID(toolset.ID))
	require.NoError(t, err)
	require.Equal(t, serverBefore, serverAfter)
	endpointAfter, err := q.GetMcpEndpointRow(ctx, deriveEndpointID(toolset.ID))
	require.NoError(t, err)
	require.Equal(t, endpointBefore, endpointAfter)
	metadataAfter, err := q.GetMetadataRow(ctx, repo.GetMetadataRowParams{ID: metadata.ID, ProjectID: tn.projectID})
	require.NoError(t, err)
	require.Equal(t, metadataBefore, metadataAfter)
	attachmentsAfter, err := q.ListCollectionAttachmentRows(ctx, collection)
	require.NoError(t, err)
	require.Equal(t, attachmentsBefore, attachmentsAfter)

	servers, err := q.CountMcpServersInProject(ctx, tn.projectID)
	require.NoError(t, err)
	require.EqualValues(t, 1, servers)
	endpoints, err := q.CountMcpEndpointsInProject(ctx, tn.projectID)
	require.NoError(t, err)
	require.EqualValues(t, 1, endpoints)
}

func TestRun_EndpointSlugCollisionBlocks(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	sharedSlug := "wrap-shared-" + uuid.NewString()[:8]

	// An unrelated toolset-backed server already owns the platform address.
	occupantBackend := tn.newToolset(t, candidateSpec{
		mcpSlug:        "",
		mcpEnabled:     false,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	q := tn.queries()
	occupantServerID := uuid.New()
	_, err := q.InsertWrapperMcpServer(ctx, repo.InsertWrapperMcpServerParams{
		ID:            occupantServerID,
		ProjectID:     tn.projectID,
		Name:          conv.ToPGText("occupant"),
		Slug:          conv.ToPGText("occupant-" + uuid.NewString()[:8]),
		EnvironmentID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:     uuid.NullUUID{UUID: occupantBackend.ID, Valid: true},
		Visibility:    "private",
	})
	require.NoError(t, err)
	_, err = q.InsertWrapperMcpEndpoint(ctx, repo.InsertWrapperMcpEndpointParams{
		ID:             uuid.New(),
		ProjectID:      tn.projectID,
		CustomDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpServerID:    occupantServerID,
		Slug:           sharedSlug,
	})
	require.NoError(t, err)

	tn.newToolset(t, candidateSpec{
		mcpSlug:        sharedSlug,
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})

	report := runWrap(t, tn, applyOptions())

	require.Len(t, report.Rows, 1)
	require.Equal(t, OutcomeBlockedCollision, report.Rows[0].Outcome)
	require.NotEmpty(t, report.Rows[0].Reason)

	servers, err := q.CountMcpServersInProject(ctx, tn.projectID)
	require.NoError(t, err)
	require.EqualValues(t, 1, servers)
	endpoints, err := q.CountMcpEndpointsInProject(ctx, tn.projectID)
	require.NoError(t, err)
	require.EqualValues(t, 1, endpoints)
}

func TestRun_DanglingEnvironmentBlocks(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)

	tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-env-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "ghost",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})

	report := runWrap(t, tn, applyOptions())

	require.Len(t, report.Rows, 1)
	require.Equal(t, OutcomeBlockedEnvironment, report.Rows[0].Outcome)

	servers, err := tn.queries().CountMcpServersInProject(t.Context(), tn.projectID)
	require.NoError(t, err)
	require.Zero(t, servers)
}

func TestRun_DeadDomainBlocksWithoutFlag(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	domainID := tn.newCustomDomain(t)
	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-dead-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: domainID, Valid: true},
	})
	tn.softDeleteCustomDomain(t)

	report := runWrap(t, tn, applyOptions())

	require.Len(t, report.Rows, 1)
	require.Equal(t, OutcomeBlockedDeadDomain, report.Rows[0].Outcome)

	q := tn.queries()
	toolsetRow, err := q.GetToolsetRow(ctx, repo.GetToolsetRowParams{ID: toolset.ID, ProjectID: tn.projectID})
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: domainID, Valid: true}, toolsetRow.CustomDomainID)

	servers, err := q.CountMcpServersInProject(ctx, tn.projectID)
	require.NoError(t, err)
	require.Zero(t, servers)
}

func TestRun_DeadDomainClearsAndWrapsPlatformWithFlag(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	domainID := tn.newCustomDomain(t)
	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-clear-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: domainID, Valid: true},
	})
	tn.softDeleteCustomDomain(t)

	opts := applyOptions()
	opts.ClearDeadDomain = true
	report := runWrap(t, tn, opts)

	require.Len(t, report.Rows, 1)
	row := report.Rows[0]
	require.Equal(t, OutcomeCreated, row.Outcome)
	require.True(t, row.ClearedDeadDomain)

	q := tn.queries()
	toolsetRow, err := q.GetToolsetRow(ctx, repo.GetToolsetRowParams{ID: toolset.ID, ProjectID: tn.projectID})
	require.NoError(t, err)
	require.False(t, toolsetRow.CustomDomainID.Valid)

	endpoint, err := q.GetMcpEndpointRow(ctx, deriveEndpointID(toolset.ID))
	require.NoError(t, err)
	require.False(t, endpoint.CustomDomainID.Valid)
}

func TestRun_LiveDomainEndpointKeepsDomain(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)

	domainID := tn.newCustomDomain(t)
	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-dom-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    true,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: domainID, Valid: true},
	})

	report := runWrap(t, tn, applyOptions())

	require.Len(t, report.Rows, 1)
	row := report.Rows[0]
	require.Equal(t, OutcomeCreated, row.Outcome)
	require.False(t, row.ClearedDeadDomain)

	endpoint, err := tn.queries().GetMcpEndpointRow(t.Context(), deriveEndpointID(toolset.ID))
	require.NoError(t, err)
	require.Equal(t, uuid.NullUUID{UUID: domainID, Valid: true}, endpoint.CustomDomainID)
}

func TestRun_MovesMetadataAndCollectionAttachmentsInPlace(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-move-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	metadata := tn.attachMetadata(t, toolset.ID)

	collection := tn.newCollection(t)
	historical := tn.attachToolsetToCollection(t, collection, toolset.ID)
	tn.detachToolsetFromCollection(t, collection, toolset.ID)
	live := tn.attachToolsetToCollection(t, collection, toolset.ID)
	require.NotEqual(t, historical.ID, live.ID)

	report := runWrap(t, tn, applyOptions())

	require.Len(t, report.Rows, 1)
	row := report.Rows[0]
	require.Equal(t, OutcomeCreated, row.Outcome)
	require.EqualValues(t, 1, row.MetadataMoved)
	require.EqualValues(t, 2, row.AttachmentsMoved)

	serverID := deriveServerID(toolset.ID)
	q := tn.queries()

	metadataRow, err := q.GetMetadataRow(ctx, repo.GetMetadataRowParams{ID: metadata.ID, ProjectID: tn.projectID})
	require.NoError(t, err)
	require.False(t, metadataRow.ToolsetID.Valid)
	require.Equal(t, uuid.NullUUID{UUID: serverID, Valid: true}, metadataRow.McpServerID)
	require.True(t, metadata.CreatedAt.Time.Equal(metadataRow.CreatedAt.Time))
	require.True(t, metadata.UpdatedAt.Time.Equal(metadataRow.UpdatedAt.Time))

	attachments, err := q.ListCollectionAttachmentRows(ctx, collection)
	require.NoError(t, err)
	require.Len(t, attachments, 2)
	for _, attachment := range attachments {
		require.False(t, attachment.ToolsetID.Valid)
		require.Equal(t, uuid.NullUUID{UUID: serverID, Valid: true}, attachment.McpServerID)
	}

	byID := map[uuid.UUID]repo.ListCollectionAttachmentRowsRow{}
	for _, attachment := range attachments {
		byID[attachment.ID] = attachment
	}
	movedHistorical, ok := byID[historical.ID]
	require.True(t, ok, "historical attachment kept its id")
	require.True(t, movedHistorical.DeletedAt.Valid, "historical attachment stays soft-deleted")
	require.True(t, historical.PublishedAt.Time.Equal(movedHistorical.PublishedAt.Time))
	require.Equal(t, historical.PublishedBy, movedHistorical.PublishedBy)

	movedLive, ok := byID[live.ID]
	require.True(t, ok, "live attachment kept its id")
	require.False(t, movedLive.DeletedAt.Valid)
	require.True(t, live.PublishedAt.Time.Equal(movedLive.PublishedAt.Time))
	require.Equal(t, live.PublishedBy, movedLive.PublishedBy)
}

func TestRun_CursorResumeWithAfterAndLimit(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)

	a := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-cur-a-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	b := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-cur-b-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	first, second := a, b
	if b.ID.String() < a.ID.String() {
		first, second = b, a
	}

	limited := applyOptions()
	limited.Limit = 1
	firstRun := runWrap(t, tn, limited)
	require.Len(t, firstRun.Rows, 1)
	require.Equal(t, first.ID, firstRun.Rows[0].ToolsetID)
	require.Equal(t, OutcomeCreated, firstRun.Rows[0].Outcome)
	require.NotNil(t, firstRun.LastCursor)
	require.Equal(t, first.ID, *firstRun.LastCursor)

	resumed := applyOptions()
	resumed.After = uuid.NullUUID{UUID: first.ID, Valid: true}
	secondRun := runWrap(t, tn, resumed)
	require.Len(t, secondRun.Rows, 1)
	require.Equal(t, second.ID, secondRun.Rows[0].ToolsetID)
	require.Equal(t, OutcomeCreated, secondRun.Rows[0].Outcome)

	servers, err := tn.queries().CountMcpServersInProject(t.Context(), tn.projectID)
	require.NoError(t, err)
	require.EqualValues(t, 2, servers)
}

func TestRun_ProjectFilterRestrictsCandidates(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	otherProject, err := projectsrepo.New(tn.pool).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name:           "wrap-other",
		Slug:           "wrap-other",
		OrganizationID: tn.orgID,
	})
	require.NoError(t, err)
	other := &tenant{pool: tn.pool, orgID: tn.orgID, projectID: otherProject.ID}

	target := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-filter-a-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	other.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-filter-b-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})

	opts := applyOptions()
	opts.ProjectID = uuid.NullUUID{UUID: tn.projectID, Valid: true}
	report := runWrap(t, tn, opts)

	require.Len(t, report.Rows, 1)
	require.Equal(t, target.ID, report.Rows[0].ToolsetID)

	otherServers, err := tn.queries().CountMcpServersInProject(ctx, otherProject.ID)
	require.NoError(t, err)
	require.Zero(t, otherServers)
}

func TestRun_NewMetadataAfterApplyBlocksRerun(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)

	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-mdc-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	tn.attachMetadata(t, toolset.ID)

	first := runWrap(t, tn, applyOptions())
	require.Len(t, first.Rows, 1)
	require.Equal(t, OutcomeCreated, first.Rows[0].Outcome)

	// A compatibility write re-creates toolset-keyed metadata after the move:
	// both sides now own a row and the rerun must refuse to merge them.
	tn.attachMetadata(t, toolset.ID)

	second := runWrap(t, tn, applyOptions())
	require.Len(t, second.Rows, 1)
	require.Equal(t, OutcomeBlockedDependentConflict, second.Rows[0].Outcome)
}

func TestRun_WrapperWithoutEndpointBlocksAmbiguous(t *testing.T) {
	t.Parallel()
	tn := seedTenant(t)
	ctx := t.Context()

	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-amb-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})

	q := tn.queries()
	_, err := q.InsertWrapperMcpServer(ctx, repo.InsertWrapperMcpServerParams{
		ID:            uuid.New(),
		ProjectID:     tn.projectID,
		Name:          conv.ToPGText("stray wrapper"),
		Slug:          conv.ToPGText("stray-" + uuid.NewString()[:8]),
		EnvironmentID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:     uuid.NullUUID{UUID: toolset.ID, Valid: true},
		Visibility:    "private",
	})
	require.NoError(t, err)

	report := runWrap(t, tn, applyOptions())

	require.Len(t, report.Rows, 1)
	require.Equal(t, OutcomeBlockedAmbiguousWrapper, report.Rows[0].Outcome)

	endpoints, err := q.CountMcpEndpointsInProject(ctx, tn.projectID)
	require.NoError(t, err)
	require.Zero(t, endpoints)
}

// The default run (no -move-dependents) wraps the toolset but leaves
// mcp_metadata and collection attachments toolset-keyed: their server-keyed
// readers deploy in a later release, and moving ownership before then would
// orphan toolset-keyed reads.
func TestRun_DefaultRunLeavesDependentOwnershipInPlace(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	tn := seedTenant(t)

	toolset := tn.newToolset(t, candidateSpec{
		mcpSlug:        "wrap-nodep-" + uuid.NewString()[:8],
		mcpEnabled:     true,
		mcpIsPublic:    false,
		defaultEnvSlug: "",
		customDomainID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	metadata := tn.attachMetadata(t, toolset.ID)

	opts := applyOptions()
	opts.MoveDependents = false
	report := runWrap(t, tn, opts)

	require.Len(t, report.Rows, 1)
	require.Equal(t, OutcomeCreated, report.Rows[0].Outcome)
	require.Zero(t, report.Rows[0].MetadataMoved)
	require.Zero(t, report.Rows[0].AttachmentsMoved)

	q := tn.queries()
	rows, err := q.ListToolsetOwnedMetadata(ctx, uuid.NullUUID{UUID: toolset.ID, Valid: true})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, metadata.ID, rows[0].ID)
}
