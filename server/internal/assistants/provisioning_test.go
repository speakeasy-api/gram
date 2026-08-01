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

// TestGetManagedAssistantHealsStaleWarmTTL verifies the lazy warm-window heal:
// an assistant left on an older, shorter warm_ttl is raised to the current
// managed default the next time it is resolved (the path the dashboard hits on
// every chat open), and the new value is persisted — not merely reflected in the
// response.
func TestGetManagedAssistantHealsStaleWarmTTL(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_managed_heal_warm_ttl")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "managed-heal-warm-ttl")

	enabled, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.NoError(t, err)

	// Simulate an assistant provisioned under the old, shorter default.
	stale := int64(60)
	require.Less(t, stale, managedAssistantWarmTTLSeconds, "test premise: stale value must be below the current default")
	_, err = assistantrepo.New(conn).UpdateAssistant(ctx, assistantrepo.UpdateAssistantParams{
		WarmTtlSeconds: pgtype.Int8{Int64: stale, Valid: true},
		AssistantID:    enabled.ID,
		ProjectID:      projectID,
	})
	require.NoError(t, err)

	got, err := core.GetManagedAssistant(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, int(managedAssistantWarmTTLSeconds), got.WarmTTLSeconds, "resolve must heal a stale warm window to the managed default")

	stored, err := assistantrepo.New(conn).GetAssistant(ctx, assistantrepo.GetAssistantParams{AssistantID: enabled.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, managedAssistantWarmTTLSeconds, stored.WarmTtlSeconds, "heal must persist the raised warm window")
}

// TestGetManagedAssistantPreservesLongerWarmTTL verifies the heal is raise-only:
// a deliberately longer warm window is never lowered to the managed default, and
// the resolve reports the true stored value rather than assuming the default.
func TestGetManagedAssistantPreservesLongerWarmTTL(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_managed_preserve_warm_ttl")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "managed-preserve-warm-ttl")

	enabled, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1")
	require.NoError(t, err)

	longer := managedAssistantWarmTTLSeconds * 2
	_, err = assistantrepo.New(conn).UpdateAssistant(ctx, assistantrepo.UpdateAssistantParams{
		WarmTtlSeconds: pgtype.Int8{Int64: longer, Valid: true},
		AssistantID:    enabled.ID,
		ProjectID:      projectID,
	})
	require.NoError(t, err)

	got, err := core.GetManagedAssistant(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, int(longer), got.WarmTTLSeconds, "resolve must not lower a longer warm window")

	stored, err := assistantrepo.New(conn).GetAssistant(ctx, assistantrepo.GetAssistantParams{AssistantID: enabled.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, longer, stored.WarmTtlSeconds, "raise-only heal must leave a longer window untouched")
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
