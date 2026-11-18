package assistants

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/audittest/repo"
	bgtriggers "github.com/speakeasy-api/gram/server/internal/background/triggers"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
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
	return NewServiceCore(logger, testenv.NewTracerProvider(t), testenv.NewMeterProvider(t), conn, nil, nil, testRuntimeBackend{backend: runtimeBackendGKE}, nil, nil, nil, telemetry.NewStub(logger), nil, newTestAuditLogger(), testIdentityService, newTestAuthzEngine(t, conn))
}

func newProvisioningProject(t *testing.T, conn *pgxpool.Pool, slug string) uuid.UUID {
	t.Helper()
	seedIdentityMembers(t, conn)
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

	first, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1", true)
	require.NoError(t, err)
	require.Equal(t, managedAssistantName("managed-idempotent"), first.Name)
	require.Equal(t, managedAssistantModel, first.Model)
	require.Equal(t, "medium", first.ReasoningEffort)
	require.Equal(t, managedAssistantInstructions, first.Instructions)
	require.NotEmpty(t, first.Instructions, "managed instructions must be embedded, not empty")

	// Nullable expansion rows retain the effective default on both managed reads.
	err = assistantrepo.New(conn).ClearAssistantReasoningEffortForTest(ctx, assistantrepo.ClearAssistantReasoningEffortForTestParams{ProjectID: projectID, AssistantID: first.ID})
	require.NoError(t, err)

	// A second enable (even by a different user) returns the same assistant.
	second, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-2", true)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID, "enable must be idempotent")
	require.Equal(t, "medium", second.ReasoningEffort)

	all, err := core.ListAssistants(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, all, 1, "only one managed assistant may exist per project")

	got, err := core.GetManagedAssistant(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, first.ID, got.ID)
	require.Equal(t, "medium", got.ReasoningEffort)
	initialAudit, err := audittest.LatestAuditLogByActionAndSubject(ctx, conn, audit.ActionAssistantReasoningUpdate, first.ID.String())
	require.NoError(t, err)
	require.JSONEq(t, `{"reasoning_effort":"medium"}`, string(initialAudit.AfterSnapshot))
	auditCount, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAssistantReasoningUpdate)
	require.NoError(t, err)
	require.EqualValues(t, 1, auditCount, "managed creation is audited once; idempotent enables do not emit updates")

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
	})
	require.NoError(t, err)
	createCanonicalWrapper(t, conn, reachable, "org-test-billing-xyz", "disabled")

	record, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1", true)
	require.NoError(t, err)

	hydrated, err := core.GetManagedAssistant(ctx, projectID)
	require.NoError(t, err)
	require.Equal(t, record.ID, hydrated.ID)
	require.Empty(t, hydrated.MCPServers, "managed assistant must not attach project toolsets by default")

	// Provisioning must not touch the project's toolsets — MCP stays as it was.
	wrapper, err := mcpserversrepo.New(conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: reachable.ID, ProjectID: projectID})
	require.NoError(t, err)
	require.Equal(t, "disabled", wrapper.Visibility, "provisioning must not auto-enable MCP on project toolsets")
}

func TestDisableManagedAssistantTearsDown(t *testing.T) {
	t.Parallel()

	conn, err := assistantsInfra.CloneTestDatabase(t, "assistants_managed_disable")
	require.NoError(t, err)
	ctx := t.Context()

	core := newProvisioningCore(t, conn)
	projectID := newProvisioningProject(t, conn, "managed-disable")

	enabled, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1", true)
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
	reenabled, err := core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1", true)
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
		int(managedAssistantWarmTTLSeconds), int(managedAssistantMaxConcurrency), StatusActive, true, nil, nil)
	require.NoError(t, err)

	_, err = core.EnableManagedAssistant(ctx, "org-test", projectID, "user-1", true)
	require.ErrorIs(t, err, ErrManagedAssistantNameTaken)

	// The feature stays off — no mapping was created.
	_, err = core.GetManagedAssistant(ctx, projectID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestManagedAssistantReasoningAuditAttribution(t *testing.T) {
	t.Parallel()
	agent := urn.NewPrincipal(urn.PrincipalTypeAgent, uuid.NewString())
	for _, tc := range []struct {
		name    string
		creator string
		context func(context.Context) context.Context
		actor   urn.Principal
		display string
		surface audit.Surface
	}{
		{"system", "", func(ctx context.Context) context.Context { return ctx }, urn.NewSystemPrincipal("assistant-configuration"), "Assistant configuration", audit.SurfaceSystem},
		{"human", "user-1", func(ctx context.Context) context.Context {
			ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{UserID: "user-2", Email: new("user-2@example.invalid")}, urn.NewPrincipal(urn.PrincipalTypeUser, "user-2"))
			return contextvalues.SetActingSurface(ctx, string(audit.SurfaceDashboard))
		}, urn.NewPrincipal(urn.PrincipalTypeUser, "user-2"), "user-2@example.invalid", audit.SurfaceDashboard},
		{"admitted_agent", "user-1", func(ctx context.Context) context.Context {
			// Public user provenance must not replace the admitted agent or label it as a human.
			ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{UserID: "user-2", Email: new("user-2@example.invalid")}, agent)
			return contextvalues.SetActingSurface(ctx, string(audit.SurfacePlatformMCP))
		}, agent, "", audit.SurfacePlatformMCP},
		{"explicit_creator", "user-1", func(ctx context.Context) context.Context { return ctx }, urn.NewPrincipal(urn.PrincipalTypeUser, "user-1"), "", audit.SurfaceUnknown},
		{"untrusted_user_provenance", "", func(ctx context.Context) context.Context {
			return contextvalues.SetAuthContext(ctx, &contextvalues.AuthContext{UserID: "user-2", Email: new("user-2@example.invalid")})
		}, urn.NewSystemPrincipal("assistant-configuration"), "Assistant configuration", audit.SurfaceSystem},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn, err := assistantsInfra.CloneTestDatabase(t, "managed_reasoning_actor_"+tc.name)
			require.NoError(t, err)
			core := newProvisioningCore(t, conn)
			projectID := newProvisioningProject(t, conn, "managed-attribution")
			ctx := tc.context(t.Context())
			record, err := core.EnableManagedAssistant(ctx, "org-test", projectID, tc.creator, false)
			require.NoError(t, err)
			log, err := audittest.LatestAuditLogByActionAndSubject(ctx, conn, audit.ActionAssistantReasoningUpdate, record.ID.String())
			require.NoError(t, err)
			require.Equal(t, string(tc.actor.Type), log.ActorType)
			require.Equal(t, tc.actor.ID, log.ActorID)
			require.Equal(t, tc.display, log.ActorDisplay)
			_, err = urn.ParsePrincipal(log.ActorType + ":" + log.ActorID)
			require.NoError(t, err, "audit actor must be a valid principal, never user with an empty ID")
			require.Equal(t, new(string(tc.surface)), log.ActingSurface)
			internal, err := auditrepo.New(conn).GetAuditLogInternalBySubject(ctx, record.ID.String())
			require.NoError(t, err)
			require.False(t, internal, "customer configuration stays visible even when a system component provisions it")
			again, err := core.EnableManagedAssistant(ctx, "org-test", projectID, tc.creator, false)
			require.NoError(t, err)
			require.Equal(t, record.ID, again.ID)
			count, err := audittest.AuditLogCountByAction(ctx, conn, audit.ActionAssistantReasoningUpdate)
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})
	}
}
