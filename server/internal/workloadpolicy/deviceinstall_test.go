package workloadpolicy_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rediscache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	srv "github.com/speakeasy-api/gram/server/gen/http/workload_identities/server"
	"github.com/speakeasy-api/gram/server/gen/types"
	gen "github.com/speakeasy-api/gram/server/gen/workload_identities"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/cache"
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
		HooksProjectSlug: types.Slug(slug),
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
	require.Equal(t, code, installCode(t, res.DownloadCommand))

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
	ttl, err := ti.redis.TTL(ctx, fmt.Sprintf("deviceInstall:%x:", sha256.Sum256([]byte(installCode(t, long.Command))))).Result()
	require.NoError(t, err)
	require.InDelta(t, (15 * time.Minute).Seconds(), ttl.Seconds(), 5)

	// A token that dies first: the code dies with it.
	short, err := ti.service.CreateDeviceInstallCommand(ctx, installCommandPayload(agentID, slug, 60))
	require.NoError(t, err)
	require.Equal(t, short.TokenExpiresAt, short.CodeExpiresAt)
	ttl, err = ti.redis.TTL(ctx, fmt.Sprintf("deviceInstall:%x:", sha256.Sum256([]byte(installCode(t, short.Command))))).Result()
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

// installPublicationFailure simulates an accepted Redis SET whose response is
// lost, or request cancellation between publication and the database commit.
type installPublicationFailure struct {
	cancel         context.CancelFunc
	storeError     bool
	key            string
	code           string
	token          string
	cleanupLive    bool
	cleanupBounded bool
}

func (h *installPublicationFailure) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *installPublicationFailure) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *installPublicationFailure) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "set" {
			key, _ := cmd.Args()[1].(string)
			if strings.HasPrefix(key, "deviceInstall:") {
				if err := next(ctx, cmd); err != nil {
					return err
				}
				h.key = key
				var record struct {
					Code      string `json:"code"`
					JoinToken string `json:"join_token"`
				}
				raw, ok := cmd.Args()[2].([]byte)
				if !ok {
					return errors.New("unexpected cache encoding")
				}
				if err := rediscache.New(&rediscache.Options{}).Unmarshal(raw, &record); err != nil {
					return fmt.Errorf("decode published install record: %w", err)
				}
				h.code, h.token = record.Code, record.JoinToken
				h.cancel()
				if h.storeError {
					return fmt.Errorf("lost cache write response: %s %s", h.code, h.token)
				}
				return nil
			}
		}
		if cmd.Name() == "del" && cmd.Args()[1] == h.key {
			h.cleanupLive = ctx.Err() == nil
			_, h.cleanupBounded = ctx.Deadline()
		}
		return next(ctx, cmd)
	}
}

func TestCreateDeviceInstallCommand_CleansPublicationOnIssuanceFailure(t *testing.T) {
	t.Parallel()
	for _, storeError := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit cancellation", true: "ambiguous cache failure"}[storeError], func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			withDeviceInstallFlag(ti)
			agentID := newAgent(t, ctx, ti, "install-agent")
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			workloadpolicy.SetDeviceInstallLoggerForTest(ti.service, logger, cache.NewRedisCacheAdapter(ti.redis))
			requestCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			hook := &installPublicationFailure{cancel: cancel, storeError: storeError}
			ti.redis.AddHook(hook)
			_, err := ti.service.CreateDeviceInstallCommand(requestCtx, installCommandPayload(agentID, callerProjectSlug(t, ctx), 3600))
			require.Error(t, err)
			require.NotEmpty(t, hook.key)
			require.NotEmpty(t, hook.code)
			require.NotEmpty(t, hook.token)
			require.NotContains(t, hook.key, hook.code)
			for _, secret := range []string{hook.code, hook.token} {
				require.NotContains(t, err.Error(), secret)
				require.NotContains(t, logs.String(), secret)
			}
			require.True(t, hook.cleanupLive, "cleanup must survive request cancellation")
			require.True(t, hook.cleanupBounded, "cleanup must have a deadline")
			remaining, err := ti.redis.Exists(ctx, hook.key).Result()
			require.NoError(t, err)
			require.Zero(t, remaining)
			count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionWorkloadDeviceJoinTokenCreate)
			require.NoError(t, err)
			require.Zero(t, count, "failed issuance must roll back its audit record")
		})
	}
}

func TestCreateDeviceInstallCommand_ValidatesSharedProjectSlugLimit(t *testing.T) {
	t.Parallel()
	for _, size := range []int{40, 41} {
		t.Run(strings.Repeat("a", size), func(t *testing.T) {
			t.Parallel()
			err := srv.ValidateCreateDeviceInstallCommandRequestBody(&srv.CreateDeviceInstallCommandRequestBody{
				AgentID: new(uuid.NewString()), HooksProjectSlug: new(strings.Repeat("a", size)),
			})
			if size == 40 {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "40")
			}
		})
	}
}

func writeInstallTestTool(t *testing.T, dir, name, script string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o700))
}

func TestCreateDeviceInstallCommand_CheckedDownloads(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	withDeviceInstallFlag(ti)
	agentID := newAgent(t, ctx, ti, "install-agent")
	result, err := ti.service.CreateDeviceInstallCommand(ctx, installCommandPayload(agentID, callerProjectSlug(t, ctx), 3600))
	require.NoError(t, err)
	for name, command := range map[string]string{"command": result.Command, "download": result.DownloadCommand} {
		for _, curlStatus := range []string{"0", "22"} {
			t.Run(name+"/curl-"+curlStatus, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				tmp := filepath.Join(root, "tmp")
				require.NoError(t, os.Mkdir(tmp, 0o700))
				writeInstallTestTool(t, root, "curl", `while [ "$1" != -o ]; do shift; done
printf 'touch "$INSTALL_TEST_EXECUTED"\n' >"$2"
exit "$INSTALL_TEST_CURL_STATUS"
`)
				writeInstallTestTool(t, root, "sudo", `exec "$@"`)
				oldScript := filepath.Join(root, "speakeasy-device-install.sh")
				require.NoError(t, os.WriteFile(oldScript, []byte(`touch "$INSTALL_TEST_EXECUTED"`), 0o600))
				cmd := exec.CommandContext(t.Context(), "sh", "-c", command)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "PATH="+root+":"+os.Getenv("PATH"), "TMPDIR="+tmp, "INSTALL_TEST_EXECUTED="+filepath.Join(root, "executed"), "INSTALL_TEST_CURL_STATUS="+curlStatus)
				out, err := cmd.CombinedOutput()
				if curlStatus == "0" {
					require.NoError(t, err, string(out))
					require.FileExists(t, filepath.Join(root, "executed"))
					if name == "download" {
						info, err := os.Stat(oldScript)
						require.NoError(t, err)
						require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
					}
				} else {
					require.Error(t, err, string(out))
					require.NoFileExists(t, filepath.Join(root, "executed"))
				}
				files, err := os.ReadDir(tmp)
				require.NoError(t, err)
				require.Empty(t, files, "partial credential-bearing downloads must be removed")
			})
		}
	}
}

func TestRenderDeviceInstallScript_CheckedReleaseDownloadAndPrivateBackup(t *testing.T) {
	t.Parallel()
	for _, downloadFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "private backup", true: "failed release download"}[downloadFails], func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			toolsDir := filepath.Join(root, "tools")
			require.NoError(t, os.Mkdir(toolsDir, 0o700))
			configDir := filepath.Join(root, "config")
			require.NoError(t, os.Mkdir(configDir, 0o700))
			config := filepath.Join(configDir, "managed.json")
			require.NoError(t, os.WriteFile(config, []byte("old credential"), 0o644))
			require.NoError(t, os.Chmod(config, 0o644))
			realInstall, err := exec.LookPath("install")
			require.NoError(t, err)
			writeInstallTestTool(t, toolsDir, "uname", "echo Linux")
			writeInstallTestTool(t, toolsDir, "id", `if [ "$#" -eq 1 ]; then echo 0; else echo 1000; fi`)
			writeInstallTestTool(t, toolsDir, "getent", `printf 'example:x:1000:1000::%s:/bin/sh\n' "$INSTALL_TEST_HOME"`)
			// Stop at service setup: this test must never alter a host service.
			writeInstallTestTool(t, toolsDir, "loginctl", "exit 23")
			writeInstallTestTool(t, toolsDir, "install", `if [ "$1" = -d ]; then shift; mode=directory; else mode=file; fi
if [ "$1" = -D ]; then shift; fi
while [ "$1" = -o ] || [ "$1" = -g ]; do shift 2; done
if [ "$mode" = directory ]; then exec "$INSTALL_TEST_INSTALL" -d "$@"; fi
exec "$INSTALL_TEST_INSTALL" "$@"
`)
			writeInstallTestTool(t, toolsDir, "curl", `while [ "$1" != -o ]; do shift; done
printf 'touch "$INSTALL_TEST_EXECUTED"\n' >"$2"
exit "$INSTALL_TEST_CURL_STATUS"
`)
			script, err := workloadpolicy.RenderDeviceInstallScriptForTest("https://cp.example", "hooks-project", sampleJoinToken)
			require.NoError(t, err)
			script = strings.ReplaceAll(script, "/etc/speakeasy", configDir)
			script = strings.ReplaceAll(script, "/usr/local/bin", filepath.Join(root, "bin"))
			cmd := exec.CommandContext(t.Context(), "sh", "-s", "--", "--force")
			cmd.Stdin = strings.NewReader(script)
			status := "0"
			if downloadFails {
				status = "22"
			}
			cmd.Env = append(os.Environ(), "PATH="+toolsDir+":"+os.Getenv("PATH"), "SUDO_USER=example", "INSTALL_TEST_HOME="+root, "INSTALL_TEST_INSTALL="+realInstall, "INSTALL_TEST_EXECUTED="+filepath.Join(root, "executed"), "INSTALL_TEST_CURL_STATUS="+status)
			out, err := cmd.CombinedOutput()
			require.Error(t, err, string(out))
			backups, err := filepath.Glob(config + ".bak.*")
			require.NoError(t, err)
			if downloadFails {
				require.NoFileExists(t, filepath.Join(root, "executed"))
				require.Empty(t, backups)
				content, err := os.ReadFile(config)
				require.NoError(t, err)
				require.Equal(t, "old credential", string(content))
				return
			}
			require.FileExists(t, filepath.Join(root, "executed"))
			require.Len(t, backups, 1)
			info, err := os.Stat(backups[0])
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
			content, err := os.ReadFile(config)
			require.NoError(t, err)
			require.Contains(t, string(content), sampleJoinToken)
		})
	}
}
