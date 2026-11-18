package platformmcp

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/officiallink"
)

const shadowSettingsTargetURL = "https://shadow-settings.example.test/mcp"

type shadowSettingsHarness struct {
	conn      *pgxpool.Pool
	principal Principal
	project   ResolvedProject
	shadow    *ShadowInventoryService
	plugins   *PluginsService
	service   *ShadowMCPSettingsService
}

func newShadowSettingsHarness(t *testing.T, name string) shadowSettingsHarness {
	t.Helper()
	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagPlatformMCPShadowAccessDecisions, principal.OrganizationID, true)
	references, err := newSubjectReferenceCodec(name + "-key")
	require.NoError(t, err)
	versions, err := newShadowDecisionVersionCodec(name + "-key")
	require.NoError(t, err)
	target := &accessgen.ShadowMCPInventoryServer{
		CanonicalServerURL: shadowSettingsTargetURL, FirstSeen: time.Now().UTC().Format(time.RFC3339), LastSeen: time.Now().UTC().Format(time.RFC3339),
		AccessSummary: &accessgen.ShadowMCPAccessSummary{State: "allowed", AllowedFor: "everyone", BlockedFor: "none", BlockingDefault: "none", DecisionCoverage: "none"},
	}
	shadow := &ShadowInventoryService{
		projects: postgresRiskProjectResolver{queries: platformrepo.New(conn)}, inventory: stubShadowInventory{list: &accessgen.ListShadowMCPInventoryResult{Servers: []*accessgen.ShadowMCPInventoryServer{target}, NextCursor: nil}, target: target}, reviews: stubShadowReview{},
		budget: testOperationBudget(), references: references, versions: versions, now: time.Now, settings: conn,
	}
	plugins := testPluginTargets(conn)
	service := NewShadowMCPSettingsService(conn, shadow, plugins, risk.NewPolicyMutationCore(conn, audit.NewLogger(), nil, noopRiskPolicySignaler{}, nil), officiallink.NewWriter(audit.NewLogger()), flags, NewPostgresOrganizationSlugResolver(conn), testOperationBudget())
	return shadowSettingsHarness{conn: conn, principal: principal, project: project, shadow: shadow, plugins: plugins, service: service}
}

func (h shadowSettingsHarness) defaultAccess(t *testing.T) ShadowMCPDefaultAccess {
	t.Helper()
	access, err := readShadowDefaultAccess(t.Context(), h.conn, h.principal.OrganizationID, h.project.ID, h.shadow.versions)
	require.NoError(t, err)
	return access
}

func (h shadowSettingsHarness) blockingPolicies(t *testing.T) []riskrepo.RiskPolicy {
	t.Helper()
	rows, err := riskrepo.New(h.conn).ListEnabledShadowMCPPoliciesByProject(t.Context(), h.project.ID)
	require.NoError(t, err)
	return rows
}

func (h shadowSettingsHarness) assignmentReference(t *testing.T, kind, displayName string) string {
	t.Helper()
	choices, err := h.plugins.ListPluginAssignments(t.Context(), h.principal, ListPluginAssignmentsInput{ProjectID: h.project.ID.String()})
	require.NoError(t, err)
	for _, choice := range choices.Assignments {
		if choice.Kind == kind && choice.DisplayName == displayName {
			return choice.Reference
		}
	}
	require.FailNow(t, "assignment option not found", "%s %s", kind, displayName)
	return ""
}

func requireShadowSettingCode(t *testing.T, err error, code string) {
	t.Helper()
	var settingErr *ShadowDecisionError
	require.ErrorAs(t, err, &settingErr)
	require.Equal(t, code, settingErr.Code, settingErr.Message)
}

func TestShadowDefaultAccessCreatesNarrowsAndRemovesTheManagedPolicy(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	h := newShadowSettingsHarness(t, "platform_shadow_default_access")
	role := seedAccessRole(t, ctx, h.conn, h.principal.OrganizationID, "contractors", "Contractors")
	initial := h.defaultAccess(t)
	require.Equal(t, "allowed", initial.Mode)
	require.Empty(t, initial.HeldBy)

	// An unconfirmed call previews and writes nothing.
	preview := SetShadowMCPDefaultAccessInput{ProjectID: h.project.ID.String(), Mode: "blocked", ExpectedVersion: initial.Version, IdempotencyKey: "block-everyone", Confirmed: false}
	_, err := h.service.SetDefaultAccess(ctx, h.principal, preview)
	requireShadowSettingCode(t, err, "confirmation_required")
	require.Empty(t, h.blockingPolicies(t))

	createsBefore := auditCount(t, t.Context(), h.conn, audit.ActionRiskPolicyCreate)
	block := preview
	block.Confirmed = true
	created, err := h.service.SetDefaultAccess(ctx, h.principal, block)
	require.NoError(t, err)
	require.Equal(t, "created", created.ResultCategory)
	require.False(t, created.Receipt.Replayed)
	require.Equal(t, "complete", created.StateStatus)
	require.Equal(t, "blocked", created.State.Mode)
	require.Equal(t, "everyone", created.State.Audience)
	policies := h.blockingPolicies(t)
	require.Len(t, policies, 1)
	require.Equal(t, "Shadow MCP", policies[0].Name)
	require.Equal(t, []string{"shadow_mcp"}, policies[0].Sources)
	require.Equal(t, "block_all", policies[0].ShadowMcpDisposition.String)
	require.Equal(t, "everyone", policies[0].AudienceType)
	require.InDelta(t, 8.0, policies[0].Score, 0.001)
	require.Equal(t, createsBefore+1, auditCount(t, t.Context(), h.conn, audit.ActionRiskPolicyCreate))

	// The inventory read reports the same state and version.
	listed, err := h.shadow.List(ctx, h.principal, ListShadowMCPInventoryInput{ProjectID: h.project.ID.String()})
	require.NoError(t, err)
	require.NotNil(t, listed.DefaultAccess)
	require.Equal(t, "blocked", listed.DefaultAccess.Mode)
	require.Equal(t, created.State.Version, listed.DefaultAccess.Version)
	require.Len(t, listed.Targets, 1)
	require.NotEmpty(t, listed.Targets[0].OfficialLinkVersion)

	// A retry with the same key answers from the receipt.
	replayed, err := h.service.SetDefaultAccess(ctx, h.principal, block)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	require.Equal(t, "created", replayed.ResultCategory)
	require.Len(t, h.blockingPolicies(t), 1)

	// The version read before the write is now stale.
	stale := block
	stale.IdempotencyKey = "stale-block"
	_, err = h.service.SetDefaultAccess(ctx, h.principal, stale)
	requireShadowSettingCode(t, err, "conflict")

	// Everyone with a role is ambiguous and refused.
	everyoneRef := h.assignmentReference(t, "everyone", "Everyone")
	roleRef := h.assignmentReference(t, "role", "Contractors")
	_, err = h.service.SetDefaultAccess(ctx, h.principal, SetShadowMCPDefaultAccessInput{ProjectID: h.project.ID.String(), Mode: "blocked", AudienceReferences: []string{everyoneRef, roleRef}, ExpectedVersion: h.defaultAccess(t).Version, IdempotencyKey: "everyone-and-role", Confirmed: true})
	requireShadowSettingCode(t, err, "invalid_request")

	updatesBefore := auditCount(t, t.Context(), h.conn, audit.ActionRiskPolicyUpdate)
	narrowed, err := h.service.SetDefaultAccess(ctx, h.principal, SetShadowMCPDefaultAccessInput{ProjectID: h.project.ID.String(), Mode: "blocked", AudienceReferences: []string{roleRef}, ExpectedVersion: h.defaultAccess(t).Version, IdempotencyKey: "narrow-to-role", Confirmed: true})
	require.NoError(t, err)
	require.Equal(t, "updated", narrowed.ResultCategory)
	require.Equal(t, "selected", narrowed.Audience)
	require.Len(t, narrowed.Roles, 1)
	require.Equal(t, "Contractors", narrowed.Roles[0].DisplayName)
	require.Equal(t, "selected", narrowed.State.Audience)
	require.Len(t, narrowed.State.Roles, 1)
	policies = h.blockingPolicies(t)
	require.Len(t, policies, 1)
	require.Equal(t, "targeted", policies[0].AudienceType)
	require.Equal(t, updatesBefore+1, auditCount(t, t.Context(), h.conn, audit.ActionRiskPolicyUpdate))
	current, err := loadShadowDefaultAccess(ctx, h.conn, h.principal.OrganizationID, h.project.ID)
	require.NoError(t, err)
	require.Equal(t, []string{role.String()}, current.audience)

	// The same audience again changes nothing.
	same, err := h.service.SetDefaultAccess(ctx, h.principal, SetShadowMCPDefaultAccessInput{ProjectID: h.project.ID.String(), Mode: "blocked", AudienceReferences: []string{roleRef}, ExpectedVersion: h.defaultAccess(t).Version, IdempotencyKey: "narrow-again", Confirmed: true})
	require.NoError(t, err)
	require.Equal(t, "unchanged", same.ResultCategory)
	require.Equal(t, updatesBefore+1, auditCount(t, t.Context(), h.conn, audit.ActionRiskPolicyUpdate))

	policyID := h.blockingPolicies(t)[0].ID
	cleaner := &recordingResultsCleaner{}
	h.service.WithResultsCleaner(cleaner, slog.New(slog.DiscardHandler))
	deletesBefore := auditCount(t, t.Context(), h.conn, audit.ActionRiskPolicyDelete)
	allowed, err := h.service.SetDefaultAccess(ctx, h.principal, SetShadowMCPDefaultAccessInput{ProjectID: h.project.ID.String(), Mode: "allowed", ExpectedVersion: h.defaultAccess(t).Version, IdempotencyKey: "allow-by-default", Confirmed: true})
	require.NoError(t, err)
	require.Equal(t, "removed", allowed.ResultCategory)
	require.Equal(t, "allowed", allowed.State.Mode)
	require.Empty(t, h.blockingPolicies(t))
	require.Equal(t, deletesBefore+1, auditCount(t, t.Context(), h.conn, audit.ActionRiskPolicyDelete))
	require.Equal(t, []uuid.UUID{policyID}, cleaner.cleaned)

	// Allowed again is already the state.
	again, err := h.service.SetDefaultAccess(ctx, h.principal, SetShadowMCPDefaultAccessInput{ProjectID: h.project.ID.String(), Mode: "allowed", ExpectedVersion: h.defaultAccess(t).Version, IdempotencyKey: "allow-again", Confirmed: true})
	require.NoError(t, err)
	require.Equal(t, "unchanged", again.ResultCategory)
	require.Equal(t, deletesBefore+1, auditCount(t, t.Context(), h.conn, audit.ActionRiskPolicyDelete))
	require.Len(t, cleaner.cleaned, 1)
}

// recordingResultsCleaner records the policies whose results cleanup was
// triggered.
type recordingResultsCleaner struct {
	cleaned []uuid.UUID
}

func (c *recordingResultsCleaner) Clean(_ context.Context, _, policyID uuid.UUID) error {
	c.cleaned = append(c.cleaned, policyID)
	return nil
}

func seedShadowModePolicy(t *testing.T, h shadowSettingsHarness, name string, sources []string, disposition string) riskrepo.RiskPolicy {
	t.Helper()
	row, err := riskrepo.New(h.conn).CreateRiskPolicy(t.Context(), riskrepo.CreateRiskPolicyParams{
		ID: uuid.New(), ProjectID: h.project.ID, OrganizationID: h.principal.OrganizationID, Name: name, PolicyType: "standard",
		Sources: sources, PresidioEntities: []string{}, AnalyzerConfig: []byte(`{}`), PromptInjectionRules: []string{}, DisabledRules: []string{}, CustomRuleIds: []string{},
		Enabled: true, Action: "block", AudienceType: "everyone", ShadowMcpDisposition: conv.ToPGTextEmpty(disposition), AutoName: false,
		UserMessage: pgtype.Text{}, Prompt: pgtype.Text{}, ModelConfig: nil, Score: pgtype.Float8{Float64: 5, Valid: true},
	})
	require.NoError(t, err)
	return row
}

func TestShadowDefaultAccessRefusesWhenGuardrailsHoldsTheSlot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	t.Run("shadow ai allows by default", func(t *testing.T) {
		t.Parallel()

		h := newShadowSettingsHarness(t, "platform_shadow_default_access_shadow_ai")
		held := seedShadowModePolicy(t, h, "Shadow AI", []string{"shadow_mcp"}, "allow_all")
		state := h.defaultAccess(t)
		require.Equal(t, "allowed", state.Mode)
		require.Equal(t, "shadow_ai", state.HeldBy)

		_, err := h.service.SetDefaultAccess(ctx, h.principal, SetShadowMCPDefaultAccessInput{ProjectID: h.project.ID.String(), Mode: "blocked", ExpectedVersion: state.Version, IdempotencyKey: "preview-block", Confirmed: false})
		requireShadowSettingCode(t, err, "held_by_shadow_ai")
		_, err = h.service.SetDefaultAccess(ctx, h.principal, SetShadowMCPDefaultAccessInput{ProjectID: h.project.ID.String(), Mode: "blocked", ExpectedVersion: state.Version, IdempotencyKey: "block", Confirmed: true})
		requireShadowSettingCode(t, err, "held_by_shadow_ai")

		// Allowed is what that policy already gives, so it is a no-op that
		// leaves the policy in place.
		allowed, err := h.service.SetDefaultAccess(ctx, h.principal, SetShadowMCPDefaultAccessInput{ProjectID: h.project.ID.String(), Mode: "allowed", ExpectedVersion: state.Version, IdempotencyKey: "allow", Confirmed: true})
		require.NoError(t, err)
		require.Equal(t, "unchanged", allowed.ResultCategory)
		policies := h.blockingPolicies(t)
		require.Len(t, policies, 1)
		require.Equal(t, held.ID, policies[0].ID)
		require.Equal(t, held.Version, policies[0].Version)
	})
	t.Run("guardrails scans other risks", func(t *testing.T) {
		t.Parallel()

		h := newShadowSettingsHarness(t, "platform_shadow_default_access_guardrails")
		held := seedShadowModePolicy(t, h, "Secrets and Shadow MCP", []string{"gitleaks", "shadow_mcp"}, "block_all")
		state := h.defaultAccess(t)
		require.Equal(t, "blocked", state.Mode)
		require.Equal(t, "guardrails", state.HeldBy)
		for _, mode := range []string{"allowed", "blocked"} {
			_, err := h.service.SetDefaultAccess(ctx, h.principal, SetShadowMCPDefaultAccessInput{ProjectID: h.project.ID.String(), Mode: mode, ExpectedVersion: state.Version, IdempotencyKey: "guardrails-" + mode, Confirmed: true})
			requireShadowSettingCode(t, err, "held_by_guardrails")
		}
		policies := h.blockingPolicies(t)
		require.Len(t, policies, 1)
		require.Equal(t, held.ID, policies[0].ID)
		require.Equal(t, held.Version, policies[0].Version)
	})
}

func seedShadowOfficialServer(t *testing.T, conn *pgxpool.Pool, projectID uuid.UUID, name, visibility string) uuid.UUID {
	t.Helper()
	ctx := t.Context()
	remote, err := remotemcprepo.New(conn).CreateServer(ctx, remotemcprepo.CreateServerParams{ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText(name), Slug: conv.ToPGText("remote-" + uuid.NewString()[:8]), TransportType: "streamable-http", Url: "https://official-" + uuid.NewString()[:8] + ".example.test/mcp"})
	require.NoError(t, err)
	server, err := mcpserversrepo.New(conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: conv.ToPGText(name), Slug: conv.ToPGText("official-" + uuid.NewString()[:8]),
		RemoteMcpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, Visibility: visibility,
	})
	require.NoError(t, err)
	return server.ID
}

func (h shadowSettingsHarness) linkVersion(t *testing.T) string {
	t.Helper()
	key, err := officiallink.Key(shadowSettingsTargetURL)
	require.NoError(t, err)
	link, err := officiallink.Read(t.Context(), h.conn, h.project.ID, key)
	require.NoError(t, err)
	version, err := h.shadow.versions.encodeSetting(shadowOfficialLinkVersionDomain, shadowOfficialLinkVersionState(h.project.ID, key, link))
	require.NoError(t, err)
	return version
}

func (h shadowSettingsHarness) link(t *testing.T) officiallink.Link {
	t.Helper()
	key, err := officiallink.Key(shadowSettingsTargetURL)
	require.NoError(t, err)
	link, err := officiallink.Read(t.Context(), h.conn, h.project.ID, key)
	require.NoError(t, err)
	return link
}

func (h shadowSettingsHarness) targetReference(t *testing.T, kind, key string) string {
	t.Helper()
	reference, err := h.shadow.references.EncodeScoped(h.principal, shadowTargetReferenceKind, queryScope("shadow_target", h.project.ID.String()), `{"kind":"`+kind+`","key":"`+key+`"}`, time.Now())
	require.NoError(t, err)
	return reference
}

func TestShadowOfficialServerLinksSwitchesAndRemoves(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	h := newShadowSettingsHarness(t, "platform_shadow_official_server")
	official := seedShadowOfficialServer(t, h.conn, h.project.ID, "Company Tools", "private")
	disabled := seedShadowOfficialServer(t, h.conn, h.project.ID, "Retired", "disabled")
	otherProject, err := projectsrepo.New(h.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other", Slug: "other-" + uuid.NewString()[:8], OrganizationID: h.principal.OrganizationID})
	require.NoError(t, err)
	foreign := seedShadowOfficialServer(t, h.conn, otherProject.ID, "Elsewhere", "private")
	target := h.targetReference(t, shadowTargetKindServerURL, shadowSettingsTargetURL)
	call := func(action, mcpID, version, key string, confirmed bool) (SetShadowMCPOfficialServerOutput, error) {
		return h.service.SetOfficialServer(ctx, h.principal, SetShadowMCPOfficialServerInput{ProjectID: h.project.ID.String(), TargetReference: target, Action: action, MCPID: mcpID, ExpectedVersion: version, IdempotencyKey: key, Confirmed: confirmed})
	}

	initial := h.linkVersion(t)
	_, err = call("link", official.String(), initial, "link", false)
	requireShadowSettingCode(t, err, "confirmation_required")
	require.False(t, h.link(t).Linked())

	setsBefore := auditCount(t, t.Context(), h.conn, audit.ActionShadowMCPOfficialServerSet)
	linked, err := call("link", official.String(), initial, "link", true)
	require.NoError(t, err)
	require.Equal(t, "changed", linked.ResultCategory)
	require.Equal(t, "on", linked.LinkState)
	require.Equal(t, official.String(), linked.MCPID)
	require.Equal(t, "complete", linked.StateStatus)
	require.Equal(t, officiallink.Link{MCPServerID: official, AllowDirectAccess: false}, h.link(t))
	require.Equal(t, setsBefore+1, auditCount(t, t.Context(), h.conn, audit.ActionShadowMCPOfficialServerSet))
	record, err := audittest.LatestAuditLogByAction(ctx, h.conn, audit.ActionShadowMCPOfficialServerSet)
	require.NoError(t, err)
	require.Equal(t, shadowSettingsTargetURL, record.SubjectDisplay)

	replayed, err := call("link", official.String(), initial, "link", true)
	require.NoError(t, err)
	require.True(t, replayed.Receipt.Replayed)
	_, err = call("remove", "", initial, "stale-remove", true)
	requireShadowSettingCode(t, err, "conflict")
	require.True(t, h.link(t).Linked())

	off, err := call("switch_off", "", h.linkVersion(t), "switch-off", true)
	require.NoError(t, err)
	require.Equal(t, "off", off.LinkState)
	require.Equal(t, officiallink.Link{MCPServerID: official, AllowDirectAccess: true}, h.link(t))
	on, err := call("switch_on", "", h.linkVersion(t), "switch-on", true)
	require.NoError(t, err)
	require.Equal(t, "on", on.LinkState)
	require.Equal(t, setsBefore+3, auditCount(t, t.Context(), h.conn, audit.ActionShadowMCPOfficialServerSet))

	same, err := call("link", official.String(), h.linkVersion(t), "link-again", true)
	require.NoError(t, err)
	require.Equal(t, "unchanged", same.ResultCategory)
	require.Equal(t, setsBefore+3, auditCount(t, t.Context(), h.conn, audit.ActionShadowMCPOfficialServerSet))

	_, err = call("link", disabled.String(), h.linkVersion(t), "link-disabled", true)
	requireShadowSettingCode(t, err, "invalid_request")
	_, err = call("link", foreign.String(), h.linkVersion(t), "link-foreign", true)
	requireShadowSettingCode(t, err, "not_found")
	_, err = call("link", uuid.NewString(), h.linkVersion(t), "link-missing", true)
	requireShadowSettingCode(t, err, "not_found")
	require.Equal(t, officiallink.Link{MCPServerID: official, AllowDirectAccess: false}, h.link(t))

	removed, err := call("remove", "", h.linkVersion(t), "remove", true)
	require.NoError(t, err)
	require.Equal(t, "none", removed.LinkState)
	require.Empty(t, removed.MCPID)
	require.False(t, h.link(t).Linked())
	require.Equal(t, setsBefore+4, auditCount(t, t.Context(), h.conn, audit.ActionShadowMCPOfficialServerSet))

	// With nothing linked there is no link to switch.
	_, err = call("switch_off", "", h.linkVersion(t), "switch-off-unlinked", true)
	requireShadowSettingCode(t, err, "invalid_request")
}

func TestShadowOfficialServerRefusesLocalCommands(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	h := newShadowSettingsHarness(t, "platform_shadow_official_server_stdio")
	official := seedShadowOfficialServer(t, h.conn, h.project.ID, "Company Tools", "private")
	_, err := h.service.SetOfficialServer(ctx, h.principal, SetShadowMCPOfficialServerInput{
		ProjectID: h.project.ID.String(), TargetReference: h.targetReference(t, shadowTargetKindStdioCommand, "local-command"), Action: "link", MCPID: official.String(),
		ExpectedVersion: h.linkVersion(t), IdempotencyKey: "link-stdio", Confirmed: true,
	})
	requireShadowSettingCode(t, err, "invalid_request")
	count, err := audittest.AuditLogCountByAction(ctx, h.conn, audit.ActionShadowMCPOfficialServerSet)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestShadowSettingsRequireTheRolloutFlag(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	h := newShadowSettingsHarness(t, "platform_shadow_settings_flag")
	h.service.flags = &feature.InMemory{}
	_, err := h.service.SetDefaultAccess(ctx, h.principal, SetShadowMCPDefaultAccessInput{ProjectID: h.project.ID.String(), Mode: "blocked", ExpectedVersion: h.defaultAccess(t).Version, IdempotencyKey: "flag-off", Confirmed: true})
	requireShadowSettingCode(t, err, "feature_unavailable")
	require.Empty(t, h.blockingPolicies(t))
	_, err = h.service.SetOfficialServer(ctx, h.principal, SetShadowMCPOfficialServerInput{ProjectID: h.project.ID.String(), TargetReference: h.targetReference(t, shadowTargetKindServerURL, shadowSettingsTargetURL), Action: "remove", ExpectedVersion: h.linkVersion(t), IdempotencyKey: "flag-off", Confirmed: true})
	requireShadowSettingCode(t, err, "feature_unavailable")
}
