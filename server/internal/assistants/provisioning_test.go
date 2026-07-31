package assistants

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func newProvisioningCore(t *testing.T, conn *pgxpool.Pool) *ServiceCore {
	t.Helper()
	logger := testenv.NewLogger(t)
	return NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendFlyIO}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger())
}

func newProvisioningProject(t *testing.T, conn *pgxpool.Pool, slug string) uuid.UUID {
	t.Helper()
	proj, err := projectsrepo.New(conn).CreateProject(t.Context(), projectsrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: "org-test",
	})
	require.NoError(t, err)
	return proj.ID
}

func TestEnableManagedAssistantIsIdempotent(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_managed_idempotent")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "managed-idempotent")

	first, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.NoError(t, err)
	require.Equal(t, managedAssistantName("managed-idempotent"), first.Name)
	require.Equal(t, managedAssistantModel, first.Model)
	require.Equal(t, managedAssistantInstructions, first.Instructions)
	require.NotEmpty(t, first.Instructions, "managed instructions must be embedded, not empty")

	// A second enable (even by a different user) returns the same assistant.
	second, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-2")
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID, "enable must be idempotent")

	all, err := core.ListAssistants(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, all, 1, "only one managed assistant may exist per project")

	got, err := core.GetManagedAssistant(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, first.ID, got.ID)

	triggers, err := triggerrepo.New(conn).ListActiveTriggerInstancesByTarget(ctx, triggerrepo.ListActiveTriggerInstancesByTargetParams{
		ProjectID:      projectID,
		DefinitionSlug: sourceKindDashboard,
		TargetKind:     bgtriggers.TargetKindAssistant,
		TargetRef:      first.ID.String(),
	})
	require.NoError(t, err)
	require.Len(t, triggers, 1, "re-enable must not duplicate the dashboard trigger")
}

func TestEnableManagedAssistantAttachesNoToolsets(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_managed_toolsets")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "managed-toolsets")

	toolsetsQ := toolsetsrepo.New(conn)
	// An MCP-reachable toolset already exists in the project. The managed
	// assistant must still start empty — admins add project MCP servers
	// deliberately, not by default.
	reachable, err := toolsetsQ.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         "org-test",
		ProjectID:              projectID,
		Name:                   "Billing",
		Slug:                   "billing",
		Description:            pgtype.Text{},
		DefaultEnvironmentSlug: pgtype.Text{},
		McpSlug:                pgtype.Text{String: "org-test-billing-xyz", Valid: true},
		McpEnabled:             false,
	})
	require.NoError(t, err)

	record, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.NoError(t, err)

	require.Empty(t, record.Toolsets, "managed assistant must not attach project toolsets by default")

	// Provisioning must not touch the project's toolsets — MCP stays as it was.
	reloaded, err := toolsetsQ.GetToolset(ctx, toolsetsrepo.GetToolsetParams{
		Slug:      reachable.Slug,
		ProjectID: projectID,
	})
	require.NoError(t, err)
	require.False(t, reloaded.McpEnabled, "provisioning must not auto-enable MCP on project toolsets")
}

func TestDisableManagedAssistantTearsDown(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_managed_disable")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "managed-disable")

	enabled, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.NoError(t, err)

	require.NoError(t, core.DisableManagedAssistant(ctx, projectID, urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"), nil))

	// Mapping is gone — resolver reports no managed assistant.
	_, err = core.GetManagedAssistant(ctx, projectID)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// Underlying assistant is soft-deleted (not listed).
	all, err := core.ListAssistants(ctx, projectID)
	require.NoError(t, err)
	require.Empty(t, all)

	// Disabling again is a no-op.
	require.NoError(t, core.DisableManagedAssistant(ctx, projectID, urn.NewPrincipal(urn.PrincipalTypeUser, "test-user"), nil))

	// Re-enabling provisions a fresh managed assistant.
	reenabled, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.NoError(t, err)
	require.NotEqual(t, enabled.ID, reenabled.ID, "re-enable creates a new assistant")
}

// TestGetManagedAssistantNoRows guards the fast-path sentinel: a project with
// the feature off must surface pgx.ErrNoRows so EnableManagedAssistant knows to
// create rather than error out.
func TestGetManagedAssistantNoRows(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_managed_norows")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "managed-norows")

	_, err = core.GetManagedAssistant(ctx, projectID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

// TestEnableManagedAssistantFailsWhenNameTaken: a user assistant already
// holding the managed name blocks enablement with an actionable error instead
// of silently masking it as "no managed assistant".
func TestEnableManagedAssistantFailsWhenNameTaken(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_managed_name_taken")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "managed-taken")

	// A user creates an assistant that happens to occupy the managed name.
	_, err = core.CreateAssistant(ctx, "org-test", projectID, "user-1",
		managedAssistantName("managed-taken"), managedAssistantModel, "hi", nil, nil,
		int(managedAssistantWarmTTLSeconds), int(managedAssistantMaxConcurrency), StatusActive)
	require.NoError(t, err)

	_, err = core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.ErrorIs(t, err, ErrManagedAssistantNameTaken)

	// The feature stays off — no mapping was created.
	_, err = core.GetManagedAssistant(ctx, projectID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

// TestReconcileManagedAssistantDefaultsMovesLegacyRows: managed assistants are
// written once at provisioning time, so a change to the platform defaults only
// reaches older projects through the reconcile. It must move rows still sitting
// on a previous default and leave a deliberately customised one alone.
func TestReconcileManagedAssistantDefaultsMovesLegacyRows(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_managed_reconcile")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "managed-reconcile")

	record, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.NoError(t, err)

	// A freshly provisioned assistant is already on current defaults.
	moved, err := core.ReconcileManagedAssistantDefaults(ctx, projectID)
	require.NoError(t, err)
	require.Zero(t, moved, "reconcile must be a no-op once the assistant is on current defaults")

	// Rewind it to the previous defaults, as a project provisioned before the
	// change would be.
	queries := assistantrepo.New(conn)
	_, err = queries.UpdateAssistant(ctx, assistantrepo.UpdateAssistantParams{
		Name:           pgtype.Text{},
		Model:          pgtype.Text{String: legacyManagedAssistantModels[0], Valid: true},
		Instructions:   pgtype.Text{},
		WarmTtlSeconds: pgtype.Int8{Int64: legacyManagedAssistantWarmTTLSeconds, Valid: true},
		MaxConcurrency: pgtype.Int8{},
		Status:         pgtype.Text{},
		AssistantID:    record.ID,
		ProjectID:      projectID,
	})
	require.NoError(t, err)

	moved, err = core.ReconcileManagedAssistantDefaults(ctx, projectID)
	require.NoError(t, err)
	require.EqualValues(t, 1, moved)

	updated, err := core.GetManagedAssistant(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, managedAssistantModel, updated.Model)
	require.EqualValues(t, managedAssistantWarmTTLSeconds, updated.WarmTTLSeconds)

	// A model the operator chose is not a previous default, so it survives —
	// but the warm TTL, still on a previous default, is moved independently.
	_, err = queries.UpdateAssistant(ctx, assistantrepo.UpdateAssistantParams{
		Name:           pgtype.Text{},
		Model:          pgtype.Text{String: "openai/gpt-4o-mini", Valid: true},
		Instructions:   pgtype.Text{},
		WarmTtlSeconds: pgtype.Int8{Int64: legacyManagedAssistantWarmTTLSeconds, Valid: true},
		MaxConcurrency: pgtype.Int8{},
		Status:         pgtype.Text{},
		AssistantID:    record.ID,
		ProjectID:      projectID,
	})
	require.NoError(t, err)

	moved, err = core.ReconcileManagedAssistantDefaults(ctx, projectID)
	require.NoError(t, err)
	require.EqualValues(t, 1, moved)

	customised, err := core.GetManagedAssistant(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, "openai/gpt-4o-mini", customised.Model, "an operator's model choice must survive reconcile")
	require.EqualValues(t, managedAssistantWarmTTLSeconds, customised.WarmTTLSeconds)
}
