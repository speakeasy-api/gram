package mcpfeedback_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_feedback"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpfeedback"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func getSettings(ctx context.Context, ti *testInstance, serverID string) (*types.McpFeedbackSettings, error) {
	settings, err := ti.service.GetSettings(ctx, &gen.GetSettingsPayload{
		McpServerID:      serverID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	return settings, err //nolint:wrapcheck // returned for oops-code assertions on the chain
}

func updateSettings(ctx context.Context, ti *testInstance, serverID string, enabled bool, toolName, toolDescription *string) (*types.McpFeedbackSettings, error) {
	settings, err := ti.service.UpdateSettings(ctx, &gen.UpdateSettingsPayload{
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		McpServerID:      serverID,
		Enabled:          enabled,
		ToolName:         toolName,
		ToolDescription:  toolDescription,
		ExpectedVersion:  nil,
	})
	return settings, err //nolint:wrapcheck // returned for oops-code assertions on the chain
}

func TestGetSettingsOfAServerThatNeverHadAnyReturnsTheDefaultsSwitchedOff(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedRemoteBackedServer(t, ctx, ti)

	settings, err := getSettings(ctx, ti, server.ID.String())
	require.NoError(t, err)
	require.Equal(t, server.ID.String(), settings.McpServerID)
	require.False(t, settings.Enabled)
	require.Nil(t, settings.ToolName)
	require.Nil(t, settings.ToolDescription)
	require.Equal(t, mcpfeedback.DefaultToolName, settings.DefaultToolName)
	require.Equal(t, mcpfeedback.DefaultToolDescription, settings.DefaultToolDescription)
	require.Equal(t, mcpfeedback.DefaultToolName, settings.Tool.Name)
	require.Equal(t, mcpfeedback.DefaultToolDescription, settings.Tool.Description)
	require.True(t, json.Valid([]byte(settings.Tool.InputSchema)))
	require.Nil(t, settings.UpdatedAt)
	require.Nil(t, settings.UpdatedByEmail)
}

func TestUpdateSettingsSwitchesTheToolOnWithTheDefaults(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedRemoteBackedServer(t, ctx, ti)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerFeedbackSettingsUpdate)
	require.NoError(t, err)

	updated, err := updateSettings(ctx, ti, server.ID.String(), true, nil, nil)
	require.NoError(t, err)
	require.True(t, updated.Enabled)
	require.Nil(t, updated.ToolName)
	require.Nil(t, updated.ToolDescription)
	require.Equal(t, mcpfeedback.DefaultToolName, updated.Tool.Name)
	require.NotNil(t, updated.UpdatedAt)
	require.Equal(t, authCtx.Email, updated.UpdatedByEmail)

	// Reading the settings back names the same editor and the same tool.
	fetched, err := getSettings(ctx, ti, server.ID.String())
	require.NoError(t, err)
	require.Equal(t, updated, fetched)

	// The gateway now offers the tool on this server.
	tool, offered, err := newTestRecorder(t, ti, noLimits).Tool(ctx, server.ProjectID, server.ID)
	require.NoError(t, err)
	require.True(t, offered)
	require.Equal(t, mcpfeedback.DefaultToolName, tool.Name)

	// The change is in the server's own audit history, switched off before and on after.
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMcpServerFeedbackSettingsUpdate)
	require.NoError(t, err)
	require.Equal(t, before+1, after)
	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionMcpServerFeedbackSettingsUpdate, server.ID.String())
	require.NoError(t, err)
	snapshotBefore, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	snapshotAfter, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, false, snapshotBefore["Enabled"])
	require.Equal(t, true, snapshotAfter["Enabled"])
}

func TestUpdateSettingsStoresACustomNameAndDescription(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedRemoteBackedServer(t, ctx, ti)

	updated, err := updateSettings(ctx, ti, server.ID.String(), true, new("  acme_submit_feedback "), new(" Tell the Acme Tasks team what went wrong. "))
	require.NoError(t, err)
	require.Equal(t, "acme_submit_feedback", *updated.ToolName)
	require.Equal(t, "Tell the Acme Tasks team what went wrong.", *updated.ToolDescription)
	require.Equal(t, "acme_submit_feedback", updated.Tool.Name)
	require.Equal(t, "Tell the Acme Tasks team what went wrong.", updated.Tool.Description)

	// Switching the tool off keeps what the admin wrote.
	off, err := updateSettings(ctx, ti, server.ID.String(), false, new("acme_submit_feedback"), new("Tell the Acme Tasks team what went wrong."))
	require.NoError(t, err)
	require.False(t, off.Enabled)
	require.Equal(t, "acme_submit_feedback", *off.ToolName)

	// Leaving the name and description out goes back to the defaults.
	reset, err := updateSettings(ctx, ti, server.ID.String(), true, nil, nil)
	require.NoError(t, err)
	require.Nil(t, reset.ToolName)
	require.Nil(t, reset.ToolDescription)
	require.Equal(t, mcpfeedback.DefaultToolName, reset.Tool.Name)

	record, err := audittest.LatestAuditLogByActionAndSubject(ctx, ti.conn, audit.ActionMcpServerFeedbackSettingsUpdate, server.ID.String())
	require.NoError(t, err)
	snapshotBefore, err := audittest.DecodeAuditData(record.BeforeSnapshot)
	require.NoError(t, err)
	snapshotAfter, err := audittest.DecodeAuditData(record.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "acme_submit_feedback", snapshotBefore["ToolName"])
	require.Nil(t, snapshotAfter["ToolName"])
}

// A server that only repeats the default follows it when it changes.
func TestUpdateSettingsStoresARepeatedDefaultAsUnset(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedRemoteBackedServer(t, ctx, ti)

	updated, err := updateSettings(ctx, ti, server.ID.String(), true, new(mcpfeedback.DefaultToolName), new(mcpfeedback.DefaultToolDescription))
	require.NoError(t, err)
	require.Nil(t, updated.ToolName)
	require.Nil(t, updated.ToolDescription)
}

func TestUpdateSettingsRefusesWhatAgentsCouldNotCall(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedRemoteBackedServer(t, ctx, ti)

	cases := map[string]struct {
		toolName        *string
		toolDescription *string
		message         string
	}{
		"name with a space":    {toolName: new("submit feedback"), toolDescription: nil, message: "tool name may only contain letters, digits, underscores and hyphens"},
		"name too long":        {toolName: new(strings.Repeat("a", mcpfeedback.MaxToolNameLength+1)), toolDescription: nil, message: "tool name must be at most 64 characters"},
		"description too long": {toolName: nil, toolDescription: new(strings.Repeat("a", mcpfeedback.MaxToolDescriptionLength+1)), message: "tool description must be at most 2000 characters"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := updateSettings(ctx, ti, server.ID.String(), true, tc.toolName, tc.toolDescription)
			requireOopsCode(t, err, oops.CodeInvalid)
			require.ErrorContains(t, err, tc.message)
		})
	}

	settings, err := getSettings(ctx, ti, server.ID.String())
	require.NoError(t, err)
	require.False(t, settings.Enabled, "a refused change stores nothing")
}

// The gateway can only add a tool to a server whose traffic it carries.
func TestUpdateSettingsRefusesToSwitchTheToolOnForACataloguedServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedCataloguedServer(t, ctx, ti)

	_, err := updateSettings(ctx, ti, server.ID.String(), true, nil, nil)
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "does not go through Speakeasy")

	// Switching it off is still allowed, so a server that stopped being
	// proxied can be cleaned up.
	off, err := updateSettings(ctx, ti, server.ID.String(), false, nil, nil)
	require.NoError(t, err)
	require.False(t, off.Enabled)
}

func TestSettingsRejectAnUnknownOrForeignServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedRemoteBackedServer(t, ctx, ti)

	_, err := getSettings(ctx, ti, uuid.NewString())
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = updateSettings(ctx, ti, uuid.NewString(), true, nil, nil)
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = getSettings(ctx, ti, "not-a-uuid")
	requireOopsCode(t, err, oops.CodeBadRequest)

	// The same server, asked for from another project, does not exist.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	otherProject := uuid.New()
	otherAuth := *authCtx
	otherAuth.ProjectID = &otherProject
	otherCtx := contextvalues.SetAuthContext(ctx, &otherAuth)

	_, err = getSettings(otherCtx, ti, server.ID.String())
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = updateSettings(otherCtx, ti, server.ID.String(), true, nil, nil)
	requireOopsCode(t, err, oops.CodeNotFound)

	settings, err := getSettings(ctx, ti, server.ID.String())
	require.NoError(t, err)
	require.False(t, settings.Enabled)
}

func TestSettingsFollowAccessToTheServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedRemoteBackedServer(t, ctx, ti)
	other := seedRemoteBackedServer(t, ctx, ti)

	noGrants := authztest.WithExactGrants(t, ctx)
	_, err := getSettings(noGrants, ti, server.ID.String())
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = updateSettings(noGrants, ti, server.ID.String(), true, nil, nil)
	requireOopsCode(t, err, oops.CodeForbidden)
	// Without access to any server, an id that does not exist reads the same
	// as one that does.
	_, err = getSettings(noGrants, ti, uuid.NewString())
	requireOopsCode(t, err, oops.CodeForbidden)

	reader := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPRead, server.ID.String()))
	_, err = getSettings(reader, ti, server.ID.String())
	require.NoError(t, err)
	_, err = updateSettings(reader, ti, server.ID.String(), true, nil, nil)
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = getSettings(reader, ti, other.ID.String())
	requireOopsCode(t, err, oops.CodeForbidden)

	writer := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeMCPWrite, server.ID.String()))
	updated, err := updateSettings(writer, ti, server.ID.String(), true, nil, nil)
	require.NoError(t, err)
	require.True(t, updated.Enabled)
	_, err = updateSettings(writer, ti, other.ID.String(), true, nil, nil)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// A change made from settings someone else has since changed is refused, so
// it cannot silently undo theirs; one made from the latest settings is stored.
func TestUpdateSettingsRefusesAChangeMadeFromStaleSettings(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	server := seedRemoteBackedServer(t, ctx, ti)

	update := func(expected *string, toolName string) (*types.McpFeedbackSettings, error) {
		settings, err := ti.service.UpdateSettings(ctx, &gen.UpdateSettingsPayload{
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
			McpServerID:      server.ID.String(),
			Enabled:          true,
			ToolName:         &toolName,
			ToolDescription:  nil,
			ExpectedVersion:  expected,
		})
		return settings, err //nolint:wrapcheck // returned for oops-code assertions on the chain
	}

	unopened, err := getSettings(ctx, ti, server.ID.String())
	require.NoError(t, err)
	require.Empty(t, unopened.Version, "a server that never had settings has no version")

	// Two admins open the server before either saves: the first save is
	// stored, and the second, made from the same empty settings, is refused.
	first, err := update(&unopened.Version, "acme_feedback")
	require.NoError(t, err)
	require.NotEmpty(t, first.Version)
	_, err = update(&unopened.Version, "acme_notes")
	requireOopsCode(t, err, oops.CodeConflict)

	changed, err := update(&first.Version, "acme_report")
	require.NoError(t, err)
	require.NotEqual(t, first.Version, changed.Version)

	_, err = update(&first.Version, "acme_notes")
	requireOopsCode(t, err, oops.CodeConflict)

	current, err := getSettings(ctx, ti, server.ID.String())
	require.NoError(t, err)
	require.Equal(t, "acme_report", *current.ToolName, "the refused changes stored nothing")
	require.Equal(t, changed.Version, current.Version)

	// A change that names no version is stored whatever came before.
	_, err = update(nil, "acme_feedback")
	require.NoError(t, err)
}
