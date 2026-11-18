package platformmcp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/officiallink"
)

func shadowModePolicy(policyType string, sources []string, disposition string) *riskrepo.RiskPolicy {
	return &riskrepo.RiskPolicy{ID: uuid.New(), Name: "Shadow MCP", PolicyType: policyType, Sources: sources, Action: "block", Enabled: true, AudienceType: "everyone", ShadowMcpDisposition: conv.ToPGTextEmpty(disposition)}
}

// The mode and who holds it mirror the dashboard's shadowMCPModeOf.
func TestShadowDefaultAccessModeMirrorsDashboard(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		row    *riskrepo.RiskPolicy
		mode   string
		heldBy string
	}{
		{name: "no policy is allowed by default", row: nil, mode: "allowed", heldBy: ""},
		{name: "managed block all is blocked", row: shadowModePolicy("standard", []string{"shadow_mcp"}, "block_all"), mode: "blocked", heldBy: ""},
		{name: "legacy blocking policy without a disposition is blocked", row: shadowModePolicy("standard", []string{"shadow_mcp"}, ""), mode: "blocked", heldBy: ""},
		{name: "shadow ai allow all holds the slot", row: shadowModePolicy("standard", []string{"shadow_mcp"}, "allow_all"), mode: "allowed", heldBy: "shadow_ai"},
		{name: "guardrails block all holds the slot", row: shadowModePolicy("standard", []string{"gitleaks", "shadow_mcp"}, "block_all"), mode: "blocked", heldBy: "guardrails"},
		{name: "guardrails allow all holds the slot", row: shadowModePolicy("standard", []string{"gitleaks", "shadow_mcp"}, "allow_all"), mode: "allowed", heldBy: "guardrails"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			mode, heldBy := shadowDefaultAccessCurrent{row: test.row, audience: nil}.mode()
			require.Equal(t, test.mode, mode)
			require.Equal(t, test.heldBy, heldBy)
		})
	}
}

func TestShadowDefaultAccessHeldRefusals(t *testing.T) {
	t.Parallel()

	guardrails := shadowDefaultAccessCurrent{row: shadowModePolicy("standard", []string{"gitleaks", "shadow_mcp"}, "block_all"), audience: nil}
	for _, mode := range []string{"allowed", "blocked"} {
		var settingErr *ShadowDecisionError
		require.ErrorAs(t, shadowDefaultAccessHeldRefusal(guardrails, mode, "guardrails"), &settingErr)
		require.Equal(t, "held_by_guardrails", settingErr.Code)
	}

	shadowAI := shadowDefaultAccessCurrent{row: shadowModePolicy("standard", []string{"shadow_mcp"}, "allow_all"), audience: nil}
	var settingErr *ShadowDecisionError
	require.ErrorAs(t, shadowDefaultAccessHeldRefusal(shadowAI, "blocked", "shadow_ai"), &settingErr)
	require.Equal(t, "held_by_shadow_ai", settingErr.Code)
	// Allowed is what a Shadow AI policy that allows by default already gives.
	require.NoError(t, shadowDefaultAccessHeldRefusal(shadowAI, "allowed", "shadow_ai"))
}

func TestShadowSettingVersionsTrackStateAndDomain(t *testing.T) {
	t.Parallel()

	codec, err := newShadowDecisionVersionCodec("shadow-settings-version-key")
	require.NoError(t, err)
	projectID := uuid.New()
	row := shadowModePolicy("standard", []string{"shadow_mcp"}, "block_all")
	row.ProjectID = projectID
	row.UpdatedAt = pgtype.Timestamptz{Valid: true}
	current := shadowDefaultAccessCurrent{row: row, audience: []string{"user:all"}}
	version, err := codec.encodeSetting(shadowDefaultAccessVersionDomain, current.versionState(projectID))
	require.NoError(t, err)
	require.True(t, codec.matchSetting(shadowDefaultAccessVersionDomain, version, current.versionState(projectID)))

	narrowed := current
	narrowed.audience = []string{"role:organization:" + uuid.NewString()}
	require.False(t, codec.matchSetting(shadowDefaultAccessVersionDomain, version, narrowed.versionState(projectID)))
	require.False(t, codec.matchSetting(shadowDefaultAccessVersionDomain, version, shadowDefaultAccessCurrent{row: nil, audience: nil}.versionState(projectID)))
	require.False(t, codec.matchSetting(shadowDefaultAccessVersionDomain, version, current.versionState(uuid.New())))
	// A token for one setting never matches another setting's state.
	require.False(t, codec.matchSetting(shadowOfficialLinkVersionDomain, version, current.versionState(projectID)))

	key := "https://shadow.example.test/mcp"
	unlinked := shadowOfficialLinkVersionState(projectID, key, officiallink.Link{MCPServerID: uuid.Nil, AllowDirectAccess: false})
	linkVersion, err := codec.encodeSetting(shadowOfficialLinkVersionDomain, unlinked)
	require.NoError(t, err)
	linked := shadowOfficialLinkVersionState(projectID, key, officiallink.Link{MCPServerID: uuid.New(), AllowDirectAccess: false})
	require.False(t, codec.matchSetting(shadowOfficialLinkVersionDomain, linkVersion, linked))
	require.False(t, codec.matchSetting(shadowOfficialLinkVersionDomain, linkVersion, shadowOfficialLinkVersionState(projectID, key+"/other", officiallink.Link{MCPServerID: uuid.Nil, AllowDirectAccess: false})))
}

func TestShadowSettingsRefuseInvalidRequestsBeforeAnyRead(t *testing.T) {
	t.Parallel()

	service := &ShadowMCPSettingsService{}
	principal := Principal{UserID: "user", OrganizationID: "organization"}
	for _, test := range []struct {
		name  string
		input SetShadowMCPDefaultAccessInput
	}{
		{name: "missing version", input: SetShadowMCPDefaultAccessInput{ProjectID: "project", Mode: "blocked", IdempotencyKey: "key", Confirmed: true}},
		{name: "unknown mode", input: SetShadowMCPDefaultAccessInput{ProjectID: "project", Mode: "open", ExpectedVersion: "v", IdempotencyKey: "key", Confirmed: true}},
		{name: "allowed with an audience", input: SetShadowMCPDefaultAccessInput{ProjectID: "project", Mode: "allowed", AudienceReferences: []string{"ref"}, ExpectedVersion: "v", IdempotencyKey: "key", Confirmed: true}},
		{name: "blank audience", input: SetShadowMCPDefaultAccessInput{ProjectID: "project", Mode: "blocked", AudienceReferences: []string{" "}, ExpectedVersion: "v", IdempotencyKey: "key", Confirmed: true}},
	} {
		t.Run("default access "+test.name, func(t *testing.T) {
			t.Parallel()

			_, err := service.SetDefaultAccess(t.Context(), principal, test.input)
			var settingErr *ShadowDecisionError
			require.ErrorAs(t, err, &settingErr)
			require.Equal(t, "invalid_request", settingErr.Code)
		})
	}
	for _, test := range []struct {
		name  string
		input SetShadowMCPOfficialServerInput
	}{
		{name: "unknown action", input: SetShadowMCPOfficialServerInput{ProjectID: "project", TargetReference: "t", Action: "replace", ExpectedVersion: "v", IdempotencyKey: "key", Confirmed: true}},
		{name: "link without a server", input: SetShadowMCPOfficialServerInput{ProjectID: "project", TargetReference: "t", Action: "link", ExpectedVersion: "v", IdempotencyKey: "key", Confirmed: true}},
		{name: "switch with a server", input: SetShadowMCPOfficialServerInput{ProjectID: "project", TargetReference: "t", Action: "switch_off", MCPID: uuid.NewString(), ExpectedVersion: "v", IdempotencyKey: "key", Confirmed: true}},
		{name: "missing target", input: SetShadowMCPOfficialServerInput{ProjectID: "project", Action: "remove", ExpectedVersion: "v", IdempotencyKey: "key", Confirmed: true}},
	} {
		t.Run("official server "+test.name, func(t *testing.T) {
			t.Parallel()

			_, err := service.SetOfficialServer(t.Context(), principal, test.input)
			var settingErr *ShadowDecisionError
			require.ErrorAs(t, err, &settingErr)
			require.Equal(t, "invalid_request", settingErr.Code)
		})
	}

	_, err := service.SetDefaultAccess(t.Context(), principal, SetShadowMCPDefaultAccessInput{ProjectID: "project", Mode: "blocked", ExpectedVersion: "v", IdempotencyKey: "key", Confirmed: true})
	require.ErrorIs(t, err, ErrShadowDecisionUnavailable)
	_, err = service.SetOfficialServer(t.Context(), principal, SetShadowMCPOfficialServerInput{ProjectID: "project", TargetReference: "t", Action: "remove", ExpectedVersion: "v", IdempotencyKey: "key", Confirmed: true})
	require.ErrorIs(t, err, ErrShadowDecisionUnavailable)
}

func TestShadowSettingsReceiptsAreClosed(t *testing.T) {
	t.Parallel()

	require.True(t, validShadowDefaultAccessReceipt(ShadowDefaultAccessReceiptResult{Mode: "blocked", Audience: "everyone", Roles: []ShadowDecisionAudience{}, ResultCategory: "created"}))
	require.True(t, validShadowDefaultAccessReceipt(ShadowDefaultAccessReceiptResult{Mode: "allowed", Audience: "", Roles: []ShadowDecisionAudience{}, ResultCategory: "removed"}))
	require.False(t, validShadowDefaultAccessReceipt(ShadowDefaultAccessReceiptResult{Mode: "allowed", Audience: "everyone", Roles: []ShadowDecisionAudience{}, ResultCategory: "removed"}))
	require.False(t, validShadowDefaultAccessReceipt(ShadowDefaultAccessReceiptResult{Mode: "blocked", Audience: "everyone", Roles: nil, ResultCategory: "created"}))
	require.False(t, validShadowDefaultAccessReceipt(ShadowDefaultAccessReceiptResult{Mode: "blocked", Audience: "everyone", Roles: []ShadowDecisionAudience{}, ResultCategory: "decided"}))

	require.True(t, validShadowOfficialLinkReceipt(ShadowOfficialLinkReceiptResult{Action: "link", LinkState: "on", MCPID: uuid.NewString(), ResultCategory: "changed"}))
	require.True(t, validShadowOfficialLinkReceipt(ShadowOfficialLinkReceiptResult{Action: "remove", LinkState: "none", MCPID: "", ResultCategory: "unchanged"}))
	require.False(t, validShadowOfficialLinkReceipt(ShadowOfficialLinkReceiptResult{Action: "remove", LinkState: "none", MCPID: uuid.NewString(), ResultCategory: "changed"}))
	require.False(t, validShadowOfficialLinkReceipt(ShadowOfficialLinkReceiptResult{Action: "link", LinkState: "on", MCPID: "", ResultCategory: "changed"}))
}

func TestUnavailableShadowSettingsToolsKeepTheirContract(t *testing.T) {
	t.Parallel()

	descriptors := func(service *ShadowMCPSettingsService) map[string]Descriptor {
		reg := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "platform-mcp-test"}, nil))
		registerShadowSettingsTools(reg, service)
		byName := map[string]Descriptor{}
		for _, descriptor := range reg.Descriptors() {
			byName[descriptor.Name] = descriptor
		}
		return byName
	}
	unavailable := descriptors(nil)
	live := descriptors(liveShadowSettingsServiceStub(t))
	for _, name := range []string{operationSetShadowMCPDefaultAccess, operationSetShadowMCPOfficialServer} {
		descriptor, ok := unavailable[name]
		require.True(t, ok, name)
		require.Equal(t, ExternalAuthorizationOrgAdmin, descriptor.Meta.Authorization)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
		require.True(t, *descriptor.Annotations.DestructiveHint)
		require.True(t, descriptor.Annotations.IdempotentHint)
		require.False(t, descriptor.Annotations.ReadOnlyHint)
		// The placeholder keeps the live tool's contract: only the
		// description says it is not switched on.
		liveDescriptor, ok := live[name]
		require.True(t, ok, name)
		require.Equal(t, liveDescriptor.Meta, descriptor.Meta)
		require.Equal(t, liveDescriptor.Annotations, descriptor.Annotations)
		require.Equal(t, liveDescriptor.Title, descriptor.Title)
		require.JSONEq(t, string(liveDescriptor.InputSchema), string(descriptor.InputSchema))
		require.NotEqual(t, liveDescriptor.Description, descriptor.Description)
	}

	result, output, err := unavailableShadowSettingsTool[SetShadowMCPDefaultAccessInput, SetShadowMCPDefaultAccessOutput](t.Context(), nil, SetShadowMCPDefaultAccessInput{})
	require.NoError(t, err)
	require.Equal(t, SetShadowMCPDefaultAccessOutput{}, output)
	require.True(t, result.IsError)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.JSONEq(t, `{"code":"feature_unavailable","feature":"shadow_mcp_settings","message":"Shadow MCP access decisions are not enabled or are temporarily unavailable."}`, text.Text)
}

// The inventory carries a remote target's link and the version that guards
// it, and offers the suggested server only while nothing is linked.
func TestShadowInventoryProjectsOfficialLink(t *testing.T) {
	t.Parallel()

	codec, err := newShadowDecisionVersionCodec("shadow-official-link-key")
	require.NoError(t, err)
	service := &ShadowInventoryService{versions: codec}
	project := ResolvedProject{ID: uuid.New(), Name: "Default", Slug: "default"}
	officialID := uuid.New()
	row := stubOfficialInventoryRow("https://linked.example.test/mcp")
	row.OfficialServer = &accessgen.ShadowMCPOfficialServer{McpServerID: officialID.String(), Name: "Company Tools", Slug: new("company-tools"), DirectAccessAllowed: true}
	row.SuggestedOfficialServer = &accessgen.ShadowMCPSuggestedOfficialServer{McpServerID: uuid.NewString(), Name: "Other"}

	var summary ShadowMCPTargetSummary
	require.NoError(t, service.projectOfficialLink(project, row, &summary))
	require.Equal(t, &ShadowMCPOfficialServerLink{MCPID: officialID.String(), Name: "Company Tools", Slug: "company-tools", LinkState: "off"}, summary.OfficialServer)
	require.Nil(t, summary.SuggestedOfficialServer)
	require.True(t, codec.matchSetting(shadowOfficialLinkVersionDomain, summary.OfficialLinkVersion, shadowOfficialLinkVersionState(project.ID, "https://linked.example.test/mcp", officiallink.Link{MCPServerID: officialID, AllowDirectAccess: true})))

	unlinked := stubOfficialInventoryRow("https://unlinked.example.test/mcp/")
	suggestedID := uuid.NewString()
	unlinked.SuggestedOfficialServer = &accessgen.ShadowMCPSuggestedOfficialServer{McpServerID: suggestedID, Name: "Proxy"}
	summary = ShadowMCPTargetSummary{}
	require.NoError(t, service.projectOfficialLink(project, unlinked, &summary))
	require.Nil(t, summary.OfficialServer)
	require.Equal(t, &ShadowMCPSuggestedOfficialServer{MCPID: suggestedID, Name: "Proxy", Slug: ""}, summary.SuggestedOfficialServer)
	require.True(t, codec.matchSetting(shadowOfficialLinkVersionDomain, summary.OfficialLinkVersion, shadowOfficialLinkVersionState(project.ID, "https://unlinked.example.test/mcp", officiallink.Link{MCPServerID: uuid.Nil, AllowDirectAccess: false})))
}

func stubOfficialInventoryRow(canonicalURL string) *accessgen.ShadowMCPInventoryServer {
	return &accessgen.ShadowMCPInventoryServer{CanonicalServerURL: canonicalURL}
}

// liveShadowSettingsServiceStub satisfies the live registration's checks
// without a database; its handlers are never called.
func liveShadowSettingsServiceStub(t *testing.T) *ShadowMCPSettingsService {
	t.Helper()
	shadow, err := NewShadowInventoryService(stubShadowInventory{}, stubShadowReview{}, platformrepo.New(nil), testOperationBudget(), "shadow-settings-stub-key")
	require.NoError(t, err)
	service := NewShadowMCPSettingsService(nil, shadow, testPluginTargets(nil), policycore.New(nil), officiallink.NewWriter(audit.NewLogger()), &feature.InMemory{}, NewPostgresOrganizationSlugResolver(nil), testOperationBudget())
	service.db = new(pgxpool.Pool)
	return service
}

func TestShadowSettingPolicyErrorsNameTheFixForNarrowStandingDenials(t *testing.T) {
	t.Parallel()

	err := mapShadowSettingPolicyError(&policycore.MutationError{Message: "honor standing approval decisions on policy create", Cause: mcpapproval.ErrNarrowDenialUnderEveryoneBlockAll})
	require.ErrorIs(t, err, ErrShadowDecisionInvalid)
	require.Contains(t, err.Error(), "decide_shadow_mcp_access")
}
