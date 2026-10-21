package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/auth/deviceagenttoken"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpauthz/mcpauthztest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/workloadidentity"
	workloadpolicyrepo "github.com/speakeasy-api/gram/server/internal/workloadpolicy/repo"
)

// The relay on a headless box authenticates claude and ingest hooks with its
// device-agent token. These requests arrive with no prior authentication, so
// the handlers authenticate them themselves.

func bearer(token string) *string {
	v := "Bearer " + token
	return &v
}

func workloadClaudePayload(workload hooksWorkload, project string) *gen.ClaudePayload {
	sessionID := uuid.NewString()
	toolName := "Shell"
	toolUseID := "tool-" + uuid.NewString()
	return &gen.ClaudePayload{
		HookEventName:    "PostToolUse",
		SessionID:        &sessionID,
		ToolName:         &toolName,
		ToolUseID:        &toolUseID,
		DeviceAgentToken: bearer(workload.token),
		ProjectSlugInput: &project,
	}
}

func workloadIngestPayload(workload hooksWorkload, project string) *gen.IngestPayload {
	payload := canonicalIngestPayload("claude", "session.started", "workload-"+uuid.NewString())
	payload.DeviceAgentToken = bearer(workload.token)
	payload.ProjectSlugInput = &project
	return payload
}

// The agent's org:hooks_ingest grant decides which project the relay's hooks
// may name.
func TestClaudeAndIngest_WorkloadHooksFollowTheAgentsProjectGrant(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)
	other := otherReadableProject(t, ctx, ti, workload)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	grantAgentHooksFor(t, ctx, ti, workload, *authCtx.ProjectID)

	_, err := ti.service.Claude(context.Background(), workloadClaudePayload(workload, workload.projectSlug))
	require.NoError(t, err)
	_, err = ti.service.Ingest(context.Background(), workloadIngestPayload(workload, workload.projectSlug))
	require.NoError(t, err)

	_, err = ti.service.Claude(context.Background(), workloadClaudePayload(workload, other.Slug))
	requireShareableCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Ingest(context.Background(), workloadIngestPayload(workload, other.Slug))
	requireShareableCode(t, err, oops.CodeForbidden)
}

// A relay that names no project is refused, as the other hooks methods refuse
// it.
func TestClaudeAndIngest_WorkloadWithoutGramProjectIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)

	_, err := ti.service.Claude(context.Background(), workloadClaudePayload(workload, ""))
	requireShareableCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Ingest(context.Background(), workloadIngestPayload(workload, ""))
	requireShareableCode(t, err, oops.CodeForbidden)
}

// An agent with no hooks grant leaves its workload nowhere to send hooks.
func TestClaudeAndIngest_WorkloadWithoutTheHooksGrantIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)
	revokeAgentHooks(t, ctx, ti, workload)

	_, err := ti.service.Claude(context.Background(), workloadClaudePayload(workload, workload.projectSlug))
	requireShareableCode(t, err, oops.CodeForbidden)
	_, err = ti.service.Ingest(context.Background(), workloadIngestPayload(workload, workload.projectSlug))
	requireShareableCode(t, err, oops.CodeForbidden)
}

// A revoked device's token is refused on its next request with 401, so the
// relay exchanges again (and is refused there too) instead of retrying.
func TestClaudeAndIngest_RevokedWorkloadIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)

	_, err := ti.service.Claude(context.Background(), workloadClaudePayload(workload, workload.projectSlug))
	require.NoError(t, err)

	withdrawn, err := workloadpolicyrepo.New(ti.conn).SoftDeleteWorkloadAgentAssignmentForSubject(ctx, workloadpolicyrepo.SoftDeleteWorkloadAgentAssignmentForSubjectParams{
		OrganizationID:   workload.organizationID,
		WorkloadIssuerID: workload.issuerID,
		MatchKind:        string(workloadidentity.MatchKindExact),
		Subject:          workload.subject,
	})
	require.NoError(t, err)
	require.Len(t, withdrawn, 1)

	_, err = ti.service.Claude(context.Background(), workloadClaudePayload(workload, workload.projectSlug))
	requireShareableCode(t, err, oops.CodeUnauthorized)
	_, err = ti.service.Ingest(context.Background(), workloadIngestPayload(workload, workload.projectSlug))
	requireShareableCode(t, err, oops.CodeUnauthorized)
}

// A token not signed by the platform key never reaches the unauthenticated
// paths: it is refused outright.
func TestClaudeAndIngest_ForgedWorkloadTokenIsRefused(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	workload := newHooksWorkload(t, ctx, ti)
	token, _, err := deviceagenttoken.New(mcpauthztest.NewIssuer(t), nil).Mint(workload.organizationID, urn.NewWorkloadPrincipal(uuid.New(), workload.subject), time.Now())
	require.NoError(t, err)
	forged := workload
	forged.token = token

	_, err = ti.service.Claude(context.Background(), workloadClaudePayload(forged, workload.projectSlug))
	requireShareableCode(t, err, oops.CodeUnauthorized)
	_, err = ti.service.Ingest(context.Background(), workloadIngestPayload(forged, workload.projectSlug))
	requireShareableCode(t, err, oops.CodeUnauthorized)
}

// Policies still gate a headless device: a PreToolUse a policy blocks comes
// back as a deny to the workload caller.
func TestClaude_WorkloadReceivesPolicyBlock(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}
	workload := newHooksWorkload(t, ctx, ti)

	payload := workloadClaudePayload(workload, workload.projectSlug)
	toolName := "mcp__notion__search"
	payload.HookEventName = "PreToolUse"
	payload.ToolName = &toolName
	payload.ToolInput = map[string]any{}
	payload.AdditionalData = map[string]any{
		"mcp_inventory_claude_code": "notion: https://mcp.notion.com/mcp (HTTP) - connected",
	}

	result, err := ti.service.Claude(context.Background(), payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	output, ok := result.HookSpecificOutput.(*HookSpecificOutput)
	require.True(t, ok)
	require.NotNil(t, output.PermissionDecision)
	require.Equal(t, "deny", *output.PermissionDecision)
}

func TestIngest_WorkloadReceivesPolicyBlock(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestHooksService(t)
	ti.service.productFeatures = alwaysEnabledFeatures{}
	ti.service.riskScanner = stubBlockingShadowMCPScanner{}
	workload := newHooksWorkload(t, ctx, ti)

	payload := shadowMCPDenyPayload("workload-shadow-deny-"+uuid.NewString(), "call-"+uuid.NewString())
	payload.DeviceAgentToken = bearer(workload.token)
	project := workload.projectSlug
	payload.ProjectSlugInput = &project

	result, err := ti.service.Ingest(context.Background(), payload)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "deny", result.Decision, "an unauthenticated request would be acknowledged with allow")
}

// A request with no device-agent token keeps the key paths unchanged.
func TestClaude_NonDeviceAgentBearerFallsBackToTheKeyPath(t *testing.T) {
	t.Parallel()
	_, ti := newTestHooksService(t)
	other := "Bearer not-a-device-agent-token"

	sessionID := uuid.NewString()
	toolName := "Shell"
	_, err := ti.service.Claude(context.Background(), &gen.ClaudePayload{
		HookEventName:    "PostToolUse",
		SessionID:        &sessionID,
		ToolName:         &toolName,
		DeviceAgentToken: &other,
	})
	require.NoError(t, err, "an unrecognized bearer is ignored, as before")
}
