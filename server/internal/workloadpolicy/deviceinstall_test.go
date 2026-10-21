package workloadpolicy_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/workloadpolicy"
)

const deviceInstallPathPrefix = "/v1/install/device/"

// withDeviceInstallFlag puts the test organization in the rollout.
func withDeviceInstallFlag(ti *testInstance) {
	ti.features.SetFlag(feature.FlagDeviceBootstrapInstall, ti.orgID, true)
}

func callerProjectSlug(t *testing.T, ctx context.Context) string {
	t.Helper()
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectSlug)
	return *authCtx.ProjectSlug
}

func installCommandPayload(agentID uuid.UUID, slug string, expiresIn int) *gen.CreateDeviceInstallCommandPayload {
	return &gen.CreateDeviceInstallCommandPayload{
		AgentID:          agentID.String(),
		HooksProjectSlug: slug,
		ExpiresInSeconds: expiresIn,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
}

// installCode reads the install code out of a command.
func installCode(t *testing.T, command string) string {
	t.Helper()
	_, rest, ok := strings.Cut(command, deviceInstallPathPrefix)
	require.True(t, ok, "command has no install path: %s", command)
	code, _, ok := strings.Cut(rest, "'")
	require.True(t, ok)
	require.NotEmpty(t, code)
	return code
}

// fetchInstallScript requests the install script for code through the mounted
// route.
func fetchInstallScript(t *testing.T, ti *testInstance, code string) *httptest.ResponseRecorder {
	t.Helper()
	mux := goahttp.NewMuxer()
	workloadpolicy.Attach(mux, ti.service)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, deviceInstallPathPrefix+code, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// managedConfig returns the managed config a script writes, decoded.
func managedConfig(t *testing.T, script string) (line string, decoded map[string]any) {
	t.Helper()
	_, rest, ok := strings.Cut(script, "<<'SPEAKEASY_MANAGED_CONFIG'\n")
	require.True(t, ok, "script has no managed config here-document")
	line, _, ok = strings.Cut(rest, "\nSPEAKEASY_MANAGED_CONFIG\n")
	require.True(t, ok)
	require.NoError(t, json.Unmarshal([]byte(line), &decoded))
	return line, decoded
}

func TestCreateDeviceInstallCommand_ServesTheScriptOnceAndTheTokenEnrolls(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withDeviceInstallFlag(ti)
	agentID := newAgent(t, ctx, ti, "install-agent")
	slug := callerProjectSlug(t, ctx)

	beforeCreate, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenCreate)
	require.NoError(t, err)

	res, err := ti.service.CreateDeviceInstallCommand(ctx, installCommandPayload(agentID, slug, 3600))
	require.NoError(t, err)

	afterCreate, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenCreate)
	require.NoError(t, err)
	require.Equal(t, beforeCreate+1, afterCreate)

	code := installCode(t, res.Command)
	require.Equal(t, "curl -fsSL '"+testServerURL+deviceInstallPathPrefix+code+"' | sudo sh", res.Command)
	require.Equal(t,
		"curl -fsSL '"+testServerURL+deviceInstallPathPrefix+code+"' -o speakeasy-device-install.sh\nsudo sh speakeasy-device-install.sh",
		res.DownloadCommand)

	rec := fetchInstallScript(t, ti, code)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "text/x-shellscript; charset=utf-8", rec.Header().Get("Content-Type"))
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	_, config := managedConfig(t, rec.Body.String())
	token, ok := config["join_token"].(string)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(token, "gram_dj_"))
	require.Equal(t, map[string]any{"source": "device_key", "hooks_project_slug": slug}, config["workload_identity"])

	// The result names the token but never carries it.
	for _, field := range []string{res.Command, res.DownloadCommand, res.CodeExpiresAt, res.JoinTokenID, res.TokenExpiresAt} {
		require.NotContains(t, field, token)
	}

	// The code is single use.
	require.Equal(t, http.StatusNotFound, fetchInstallScript(t, ti, code).Code)

	// The token in the script is the one the result names, and it enrolls.
	reg, err := registerDevice(ctx, ti, token, publicJWK(t, newDeviceKey(t).Public()))
	require.NoError(t, err)
	require.Equal(t, agentID.String(), reg.AgentID)

	list, err := ti.service.ListDeviceJoinTokens(ctx, &gen.ListDeviceJoinTokensPayload{
		AgentID:          new(agentID.String()),
		Cursor:           nil,
		Limit:            50,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.Equal(t, res.JoinTokenID, list.Items[0].ID)
	require.Equal(t, "redeemed", list.Items[0].Status)
}

func TestCreateDeviceInstallCommand_CodeLifetimeIsCappedByTheToken(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withDeviceInstallFlag(ti)
	agentID := newAgent(t, ctx, ti, "install-agent")
	slug := callerProjectSlug(t, ctx)

	// A token that outlives the code: the code lasts 15 minutes.
	long, err := ti.service.CreateDeviceInstallCommand(ctx, installCommandPayload(agentID, slug, 3600))
	require.NoError(t, err)
	codeExpiry, err := time.Parse(time.RFC3339, long.CodeExpiresAt)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(15*time.Minute), codeExpiry, 5*time.Second)
	ttl, err := ti.redis.TTL(ctx, "deviceInstall:"+installCode(t, long.Command)+":").Result()
	require.NoError(t, err)
	require.InDelta(t, (15 * time.Minute).Seconds(), ttl.Seconds(), 5)

	// A token that dies first: the code dies with it.
	short, err := ti.service.CreateDeviceInstallCommand(ctx, installCommandPayload(agentID, slug, 60))
	require.NoError(t, err)
	require.Equal(t, short.TokenExpiresAt, short.CodeExpiresAt)
	ttl, err = ti.redis.TTL(ctx, "deviceInstall:"+installCode(t, short.Command)+":").Result()
	require.NoError(t, err)
	require.Greater(t, ttl, time.Duration(0))
	require.LessOrEqual(t, ttl, time.Minute)
}

func TestCreateDeviceInstallCommand_RequiresWhatCreatingAJoinTokenRequires(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withDeviceInstallFlag(ti)
	agentID := newAgent(t, ctx, ti, "someone-elses-agent")
	payload := installCommandPayload(agentID, callerProjectSlug(t, ctx), 3600)

	// workload:read is not enough.
	_, err := ti.service.CreateDeviceInstallCommand(withScopes(t, ctx, ti, authz.ScopeWorkloadRead), payload)
	requireOopsCode(t, err, oops.CodeForbidden)

	// The owner needs workload:write alone.
	_, err = ti.service.CreateDeviceInstallCommand(authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeWorkloadWrite, ti.orgID)), payload)
	require.NoError(t, err)

	// Anyone else also needs agent:authorize on the agent.
	other := asUser(t, ctx, ti.userID+"-other")
	_, err = ti.service.CreateDeviceInstallCommand(authztest.WithExactGrants(t, other, authz.NewGrant(authz.ScopeWorkloadWrite, ti.orgID)), payload)
	requireOopsCode(t, err, oops.CodeForbidden)
	_, err = ti.service.CreateDeviceInstallCommand(authztest.WithExactGrants(t, other,
		authz.NewGrant(authz.ScopeWorkloadWrite, ti.orgID),
		authz.NewGrant(authz.ScopeAgentAuthorize, agentID.String()),
	), payload)
	require.NoError(t, err)

	// A key with no person behind it is refused whatever its grants.
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	noPerson := *authCtx
	noPerson.UserID = ""
	_, err = ti.service.CreateDeviceInstallCommand(contextvalues.SetAuthContext(ctx, &noPerson), payload)
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestCreateDeviceInstallCommand_RequiresDeviceAgentSync(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withDeviceInstallFlag(ti)

	bare := newAgentWithoutDeviceGrants(t, ctx, ti, "no-device-grants")
	seedAgentGrant(t, ctx, ti, bare, authz.ScopeOrgHooksIngest)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenCreate)
	require.NoError(t, err)

	_, err = ti.service.CreateDeviceInstallCommand(ctx, installCommandPayload(bare, callerProjectSlug(t, ctx), 3600))
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
	require.ErrorContains(t, err, "the agent's policy must grant org:device_agent_sync on the organization")

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenCreate)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestCreateDeviceInstallCommand_RefusesAProjectOutsideTheOrganization(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withDeviceInstallFlag(ti)
	agentID := newAgent(t, ctx, ti, "install-agent")

	_, err := ti.service.CreateDeviceInstallCommand(ctx, installCommandPayload(agentID, "no-such-project", 3600))
	requireOopsCode(t, err, oops.CodeInvalid)
	require.ErrorContains(t, err, "hooks_project_slug")
}

func TestCreateDeviceInstallCommand_IsClosedOutsideTheRollout(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	agentID := newAgent(t, ctx, ti, "install-agent")

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenCreate)
	require.NoError(t, err)

	_, err = ti.service.CreateDeviceInstallCommand(ctx, installCommandPayload(agentID, callerProjectSlug(t, ctx), 3600))
	requireOopsCode(t, err, oops.CodeNotFound)

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenCreate)
	require.NoError(t, err)
	require.Equal(t, before, after, "no token may be issued outside the rollout")
}

func TestDeviceInstallScript_UnknownCodeIsNotFound(t *testing.T) {
	t.Parallel()
	_, ti := newTestService(t)

	rec := fetchInstallScript(t, ti, "not-a-real-code")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.NotContains(t, rec.Body.String(), "not-a-real-code")
}

const sampleJoinToken = "gram_dj_sample-token-value"

func TestRenderDeviceInstallScript_TokenOnlyInTheManagedConfig(t *testing.T) {
	t.Parallel()
	script, err := workloadpolicy.RenderDeviceInstallScriptForTest("https://cp.example", "hooks-project", sampleJoinToken)
	require.NoError(t, err)

	line, config := managedConfig(t, script)
	require.Equal(t, 1, strings.Count(script, sampleJoinToken), "the token must appear once")
	require.Contains(t, line, sampleJoinToken)
	require.Equal(t, sampleJoinToken, config["join_token"])
	require.Equal(t, "https://cp.example", config["_control_plane_url"])
	require.InDelta(t, 1, config["v"], 0)
}

func TestRenderDeviceInstallScript_OmitsTheDefaultControlPlane(t *testing.T) {
	t.Parallel()
	script, err := workloadpolicy.RenderDeviceInstallScriptForTest("https://app.getgram.ai", "hooks-project", sampleJoinToken)
	require.NoError(t, err)

	_, config := managedConfig(t, script)
	require.NotContains(t, config, "_control_plane_url")
}

func TestRenderDeviceInstallScript_QuotesInterpolatedValues(t *testing.T) {
	t.Parallel()
	hostile := `https://cp.example/'"$(touch /tmp/pwned)"'` + "`id`"
	script, err := workloadpolicy.RenderDeviceInstallScriptForTest(hostile, "hooks_project-1", sampleJoinToken)
	require.NoError(t, err)

	// The assignment the script makes must yield the value verbatim.
	var assignment string
	for line := range strings.SplitSeq(script, "\n") {
		if strings.HasPrefix(line, "control_plane_url=") {
			assignment = line
		}
	}
	require.NotEmpty(t, assignment)
	out, err := exec.CommandContext(t.Context(), "sh", "-c", assignment+`; printf %s "$control_plane_url"`).Output()
	require.NoError(t, err)
	require.Equal(t, hostile, string(out))

	_, config := managedConfig(t, script)
	require.Equal(t, hostile, config["_control_plane_url"])
}

func TestRenderDeviceInstallScript_IsValidShell(t *testing.T) {
	t.Parallel()
	script, err := workloadpolicy.RenderDeviceInstallScriptForTest("https://cp.example", "hooks-project", sampleJoinToken)
	require.NoError(t, err)

	cmd := exec.CommandContext(t.Context(), "sh", "-n")
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}
