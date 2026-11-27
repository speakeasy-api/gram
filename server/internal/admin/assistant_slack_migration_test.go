package admin

import (
	"maps"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	authclientsrepo "github.com/speakeasy-api/gram/server/internal/authclients/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	envrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/platformtools"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func seedLegacySlackToolset(
	t *testing.T,
	db *pgxpool.Pool,
	orgID string,
	projectID uuid.UUID,
	name, slug, defaultEnvSlug string,
	toolURNs []urn.Tool,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	ts, err := toolsetsrepo.New(db).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID:         orgID,
		ProjectID:              projectID,
		Name:                   name,
		Slug:                   slug,
		Description:            pgtype.Text{String: "Legacy Slack toolset", Valid: true},
		DefaultEnvironmentSlug: pgtype.Text{String: defaultEnvSlug, Valid: defaultEnvSlug != ""},
	})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(db).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{
		ToolsetID:     ts.ID,
		Version:       1,
		ToolUrns:      toolURNs,
		ResourceUrns:  []urn.Resource{},
		PredecessorID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	})
	require.NoError(t, err)
	srv, err := mcpserversrepo.New(db).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID:                    ts.ID,
		ProjectID:             projectID,
		Name:                  pgtype.Text{String: name, Valid: true},
		Slug:                  pgtype.Text{String: slug, Valid: true},
		EnvironmentID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserSessionIssuerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RemoteMcpServerID:     uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TunneledMcpServerID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolsetID:             uuid.NullUUID{UUID: ts.ID, Valid: true},
		UnproxiedMcpServerID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ToolVariationsGroupID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Visibility:            "private",
		NetworkAccessMode:     pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	return ts.ID, srv.ID
}

func seedEncryptedEnvEntries(t *testing.T, db *pgxpool.Pool, enc *encryption.Client, projectID, envID uuid.UUID, values map[string]string) {
	t.Helper()
	for name, plain := range values {
		cipher, err := enc.Encrypt([]byte(plain))
		require.NoError(t, err)
		_, err = envrepo.New(db).UpsertEnvironmentEntry(t.Context(), envrepo.UpsertEnvironmentEntryParams{
			Name:          name,
			Value:         cipher,
			IsSecret:      true,
			EnvironmentID: envID,
			ProjectID:     projectID,
		})
		require.NoError(t, err)
	}
}

func TestMigrateOrganizationAssistantSlack(t *testing.T) {
	t.Parallel()
	svc, db := newAssistantMigrationService(t)
	enc := testenv.NewEncryptionClient(t)
	svc.SetApplicationEncryption(enc)

	ctx := contextvalues.SetAdminAuthContext(t.Context(), &contextvalues.AdminAuthContext{
		SessionID:   "staff-session",
		OIDCSubject: "staff-subject",
		Email:       "staff@example.invalid",
		Name:        "Staff Member",
		HD:          testAdminHD,
	})

	org := newAssistantMigrationOrg(t, db, "assistant-slack-migrate")
	otherOrg := newAssistantMigrationOrg(t, db, "assistant-slack-foreign")
	owner := org.member(true)
	otherOwner := otherOrg.member(true)

	// Dedicated assistant with Slack-only toolset + legacy env-backed Slack trigger.
	dedicated := org.legacyAssistant("Dedicated Slack", owner)
	// Two assistants sharing a Slack-only toolset: the shared toolset must be
	// detached from both (and soft-deleted only once zero refs remain; to test
	// keeping a referenced toolset alive, we attach it to an unbound assistant too).
	shared := org.legacyAssistant("Shared Slack", owner)
	noSlack := org.legacyAssistant("No Slack", owner)
	foreign := otherOrg.legacyAssistant("Foreign Slack", otherOwner)

	// Provision identities for dedicated, shared, noSlack, foreign.
	_, err := svc.MigrateOrganizationAssistantIdentities(ctx, &gen.MigrateOrganizationAssistantIdentitiesPayload{OrganizationID: org.id})
	require.NoError(t, err)
	_, err = svc.MigrateOrganizationAssistantIdentities(ctx, &gen.MigrateOrganizationAssistantIdentitiesPayload{OrganizationID: otherOrg.id})
	require.NoError(t, err)

	// Add an unbound assistant AFTER identity migration so it reports needs_identity
	// and also holds a reference to the shared Slack toolset.
	unbound := org.legacyAssistant("Unbound Slack", owner)

	// Create project environment with encrypted Slack secrets + a non-Slack secret.
	envRow, err := envrepo.New(db).CreateEnvironment(t.Context(), envrepo.CreateEnvironmentParams{
		OrganizationID: org.id,
		ProjectID:      org.project,
		Name:           "Assistant Env",
		Slug:           "assistant-env",
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	const (
		botSecret     = "xoxb-legacy-bot-secret"
		userSecret    = "xoxp-legacy-user-secret"
		signingSecret = "legacy-signing-secret"
		otherSecret   = "keep-me-other-integration-key"
	)
	seedEncryptedEnvEntries(t, db, enc, org.project, envRow.ID, map[string]string{
		authclients.SlackHeaderBotToken:      botSecret,
		authclients.SlackHeaderUserToken:     userSecret,
		authclients.SlackHeaderSigningSecret: signingSecret,
		"OTHER_API_KEY":                      otherSecret,
	})

	slackTools := []urn.Tool{
		urn.NewTool(urn.ToolKindPlatform, "slack", "send_message"),
		urn.NewTool(urn.ToolKindPlatform, "slack", "read_channel_messages"),
	}
	dedicatedToolsetID, dedicatedServerID := seedLegacySlackToolset(t, db, org.id, org.project, "Dedicated Slack", "dedicated-slack", envRow.Slug, slackTools)
	sharedToolsetID, sharedServerID := seedLegacySlackToolset(t, db, org.id, org.project, "Shared Slack", "shared-slack", envRow.Slug, slackTools)

	_, err = assistantrepo.New(db).AddAssistantMcpServers(t.Context(), []assistantrepo.AddAssistantMcpServersParams{
		{AssistantID: dedicated, McpServerID: dedicatedServerID, EnvironmentID: uuid.NullUUID{UUID: envRow.ID, Valid: true}, ProjectID: org.project},
		{AssistantID: shared, McpServerID: sharedServerID, EnvironmentID: uuid.NullUUID{UUID: envRow.ID, Valid: true}, ProjectID: org.project},
		{AssistantID: unbound, McpServerID: sharedServerID, EnvironmentID: uuid.NullUUID{UUID: envRow.ID, Valid: true}, ProjectID: org.project},
	})
	require.NoError(t, err)

	dedicatedTrig, err := triggerrepo.New(db).CreateTriggerInstance(t.Context(), triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: org.id,
		ProjectID:      org.project,
		DefinitionSlug: "slack",
		Name:           "Slack",
		EnvironmentID:  uuid.NullUUID{UUID: envRow.ID, Valid: true},
		AuthClientID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TargetKind:     "assistant",
		TargetRef:      dedicated.String(),
		TargetDisplay:  "Dedicated Slack",
		ConfigJson:     []byte(`{"routing":"threads"}`),
		Status:         "active",
	})
	require.NoError(t, err)

	unboundTrig, err := triggerrepo.New(db).CreateTriggerInstance(t.Context(), triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: org.id,
		ProjectID:      org.project,
		DefinitionSlug: "slack",
		Name:           "Slack",
		EnvironmentID:  uuid.NullUUID{UUID: envRow.ID, Valid: true},
		AuthClientID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TargetKind:     "assistant",
		TargetRef:      unbound.String(),
		TargetDisplay:  "Unbound Slack",
		ConfigJson:     []byte(`{"routing":"mentions"}`),
		Status:         "active",
	})
	require.NoError(t, err)

	foreignEnv, err := envrepo.New(db).CreateEnvironment(t.Context(), envrepo.CreateEnvironmentParams{
		OrganizationID: otherOrg.id,
		ProjectID:      otherOrg.project,
		Name:           "Foreign Env",
		Slug:           "foreign-env",
		Description:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	foreignTrig, err := triggerrepo.New(db).CreateTriggerInstance(t.Context(), triggerrepo.CreateTriggerInstanceParams{
		OrganizationID: otherOrg.id,
		ProjectID:      otherOrg.project,
		DefinitionSlug: "slack",
		Name:           "Slack",
		EnvironmentID:  uuid.NullUUID{UUID: foreignEnv.ID, Valid: true},
		AuthClientID:   uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TargetKind:     "assistant",
		TargetRef:      foreign.String(),
		TargetDisplay:  "Foreign Slack",
		ConfigJson:     []byte(`{}`),
		Status:         "active",
	})
	require.NoError(t, err)

	// Run migration for org.
	run, err := svc.MigrateOrganizationAssistantSlack(ctx, &gen.MigrateOrganizationAssistantSlackPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Equal(t, org.id, run.OrganizationID)
	require.Equal(t, int64(2), run.Migrated)
	require.Zero(t, run.AlreadyMigrated)
	require.Equal(t, int64(1), run.NoSlack)
	require.Equal(t, int64(1), run.NeedsIdentity)
	require.Zero(t, run.Failed)

	byID := make(map[string]*gen.AdminAssistantSlackMigrationResult, len(run.Results))
	for _, r := range run.Results {
		byID[r.AssistantID] = r
	}

	// Dedicated assistant: migrated, 1 trigger repointed, 1 toolset detached + deleted.
	dedRes := byID[dedicated.String()]
	require.NotNil(t, dedRes)
	require.Equal(t, assistantSlackMigrationMigrated, dedRes.Outcome)
	require.NotNil(t, dedRes.AuthClientID)
	require.Equal(t, int64(1), dedRes.TriggersRepointed)
	require.Equal(t, int64(1), dedRes.ToolsetsDetached)
	require.Equal(t, int64(1), dedRes.ToolsetsDeleted)

	// Shared assistant: migrated, 0 triggers, 1 toolset detached, 0 deleted (still used by unbound).
	sharedRes := byID[shared.String()]
	require.NotNil(t, sharedRes)
	require.Equal(t, assistantSlackMigrationMigrated, sharedRes.Outcome)
	require.NotNil(t, sharedRes.AuthClientID)
	require.Equal(t, int64(0), sharedRes.TriggersRepointed)
	require.Equal(t, int64(1), sharedRes.ToolsetsDetached)
	require.Equal(t, int64(0), sharedRes.ToolsetsDeleted)

	// No-slack and unbound outcomes.
	require.Equal(t, assistantSlackMigrationNoSlack, byID[noSlack.String()].Outcome)
	require.Equal(t, assistantSlackMigrationNeedsIdentity, byID[unbound.String()].Outcome)

	// Verify dedicated slack_app auth client headers decrypt cleanly and source is "admin".
	dedClientID, err := uuid.Parse(*dedRes.AuthClientID)
	require.NoError(t, err)
	store := authclients.NewSlackAppCredentialStore(db, nil, enc)
	creds, err := store.LoadSlackAppCredentials(t.Context(), org.id, org.project, dedClientID)
	require.NoError(t, err)
	require.Equal(t, botSecret, creds[authclients.SlackHeaderBotToken])
	require.Equal(t, userSecret, creds[authclients.SlackHeaderUserToken])
	require.Equal(t, signingSecret, creds[authclients.SlackHeaderSigningSecret])

	lockedClient, err := authclientsrepo.New(db).GetAuthClient(t.Context(), authclientsrepo.GetAuthClientParams{
		ID:             dedClientID,
		OrganizationID: org.id,
	})
	require.NoError(t, err)
	require.Equal(t, authclients.SourceAdmin, lockedClient.Source, "migrated slack_app client must remain editable by org admins")

	// Verify dedicated agent holds permanent mcp:connect on assistant-slack and mcp:auth on dedClientID.
	states, err := assistantidentity.States(t.Context(), db, org.project, []uuid.UUID{dedicated, shared, noSlack})
	require.NoError(t, err)
	dedAgentID := *states[dedicated].AgentID
	dedGrants, err := agentrepo.New(db).ListAgentPolicyGrants(t.Context(), agentrepo.ListAgentPolicyGrantsParams{
		OrganizationID: org.id,
		AgentID:        dedAgentID,
	})
	require.NoError(t, err)
	require.Contains(t, builtinGrantIDsBySlug(t, dedGrants, org.project), platformtools.AssistantSlackPlatformToolsetSlug)
	wantAuthSel := assistantidentity.AssistantSlackAuthGrant(dedClientID).Selector
	var foundAuthGrant bool
	for _, g := range dedGrants {
		if g.Scope != string(authz.ScopeMCPAuth) || g.ExpiresAt.Valid {
			continue
		}
		gotSel, selErr := authz.SelectorFromRow(g.Selectors)
		require.NoError(t, selErr)
		if maps.Equal(gotSel, wantAuthSel) {
			foundAuthGrant = true
			break
		}
	}
	require.True(t, foundAuthGrant, "dedicated assistant agent must hold permanent mcp:auth on its slack_app client")

	// Verify HasAssistantSlackPlatformGrant reports true for dedicated (fallback path) and false for noSlack.
	hasGrant, err := assistantrepo.New(db).HasAssistantSlackPlatformGrant(t.Context(), assistantrepo.HasAssistantSlackPlatformGrantParams{
		OrganizationID: org.id,
		ProjectID:      org.project,
		AssistantID:    uuid.NullUUID{UUID: dedicated, Valid: true},
		ToolsetID:      platformtools.PlatformToolsetID(platformtools.AssistantSlackPlatformToolsetSlug).String(),
	})
	require.NoError(t, err)
	require.True(t, hasGrant)
	hasGrant, err = assistantrepo.New(db).HasAssistantSlackPlatformGrant(t.Context(), assistantrepo.HasAssistantSlackPlatformGrantParams{
		OrganizationID: org.id,
		ProjectID:      org.project,
		AssistantID:    uuid.NullUUID{UUID: noSlack, Valid: true},
		ToolsetID:      platformtools.PlatformToolsetID(platformtools.AssistantSlackPlatformToolsetSlug).String(),
	})
	require.NoError(t, err)
	require.False(t, hasGrant)

	// Verify trigger repointed: auth_client_id set, environment_id cleared, config preserved.
	gotTrig, err := triggerrepo.New(db).GetTriggerInstanceByIDPublic(t.Context(), dedicatedTrig.ID)
	require.NoError(t, err)
	require.True(t, gotTrig.AuthClientID.Valid)
	require.Equal(t, dedClientID, gotTrig.AuthClientID.UUID)
	require.False(t, gotTrig.EnvironmentID.Valid)
	require.JSONEq(t, `{"routing":"threads"}`, string(gotTrig.ConfigJson))

	// Unbound trigger and foreign trigger untouched.
	gotUnboundTrig, err := triggerrepo.New(db).GetTriggerInstanceByIDPublic(t.Context(), unboundTrig.ID)
	require.NoError(t, err)
	require.True(t, gotUnboundTrig.EnvironmentID.Valid)
	require.False(t, gotUnboundTrig.AuthClientID.Valid)

	gotForeignTrig, err := triggerrepo.New(db).GetTriggerInstanceByIDPublic(t.Context(), foreignTrig.ID)
	require.NoError(t, err)
	require.True(t, gotForeignTrig.EnvironmentID.Valid)
	require.False(t, gotForeignTrig.AuthClientID.Valid)

	// Dedicated Slack-only toolset and its hosted MCP server were soft-deleted; shared toolset stayed live.
	_, err = toolsetsrepo.New(db).GetToolsetByIDAndProject(t.Context(), toolsetsrepo.GetToolsetByIDAndProjectParams{ID: dedicatedToolsetID, ProjectID: org.project})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = mcpserversrepo.New(db).GetMCPServerByIDAndProjectID(t.Context(), mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: dedicatedServerID, ProjectID: org.project})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = toolsetsrepo.New(db).GetToolsetByIDAndProject(t.Context(), toolsetsrepo.GetToolsetByIDAndProjectParams{ID: sharedToolsetID, ProjectID: org.project})
	require.NoError(t, err, "shared toolset still referenced by unbound assistant must not be deleted")

	// Environment and its entries were NOT deleted.
	envEntries, err := envrepo.New(db).ListEnvironmentEntries(t.Context(), envrepo.ListEnvironmentEntriesParams{
		ProjectID:     org.project,
		EnvironmentID: envRow.ID,
	})
	require.NoError(t, err)
	require.Len(t, envEntries, 4, "migration must never delete environment entries")

	// Audit log contains zero plaintext secrets.
	authAudit, err := audittest.LatestAuditLogByActionAndSubject(t.Context(), db, audit.ActionAuthClientCreate, dedClientID.String())
	require.NoError(t, err)
	require.NotContains(t, string(authAudit.AfterSnapshot), botSecret)
	require.NotContains(t, string(authAudit.AfterSnapshot), userSecret)
	require.NotContains(t, string(authAudit.AfterSnapshot), signingSecret)

	// Re-running migration is idempotent.
	again, err := svc.MigrateOrganizationAssistantSlack(ctx, &gen.MigrateOrganizationAssistantSlackPayload{OrganizationID: org.id})
	require.NoError(t, err)
	require.Zero(t, again.Migrated)
	require.Equal(t, int64(2), again.AlreadyMigrated)
	require.Equal(t, int64(1), again.NoSlack)
	require.Equal(t, int64(1), again.NeedsIdentity)
	require.Zero(t, again.Failed)
	for _, r := range again.Results {
		if r.AssistantID == dedicated.String() {
			require.NotNil(t, r.AuthClientID)
			require.Equal(t, *dedRes.AuthClientID, *r.AuthClientID)
			require.Zero(t, r.TriggersRepointed)
			require.Zero(t, r.ToolsetsDetached)
			require.Zero(t, r.ToolsetsDeleted)
		}
	}
}
