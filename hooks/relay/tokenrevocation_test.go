package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/agenthookstest"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// revocableServer accepts every event until revoke is called, then answers 401
// as the control plane does for a withdrawn device.
func revocableServer(t *testing.T) (*fakeServer, func()) {
	t.Helper()
	var revoked atomic.Bool
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		if revoked.Load() {
			return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: "device-agent token refused"}
		}
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	return fs, func() { revoked.Store(true) }
}

func requireDenied(t *testing.T, res agenthookstest.Result, msg string) {
	t.Helper()
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, msg)
}

// A revoked token box must stay closed in a fail-open org: on the rejection
// itself, on every later event, and after the rejected token's cache expires.
func TestRevokedTokenBoxNeverFailsOpen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		refresh func(fc *fakeTokenCommand)
	}{
		{name: "refresh command fails", refresh: func(fc *fakeTokenCommand) { fc.fail = errors.New("exit status 1") }},
		{name: "refresh command refuses", refresh: func(fc *fakeTokenCommand) { fc.fail = fmt.Errorf("%w: exit status 3", errTokenRefused) }},
		{name: "refreshed token is also rejected", refresh: func(*fakeTokenCommand) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs, revoke := revocableServer(t)
			cfg, fc := tokenBox(t, fs.URL)
			stateHome := setSpoolStateHome(t)
			writeOrgSettings(cfg, true)

			res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
			require.Equal(t, "{}", strings.TrimSpace(string(res.Stdout)), "an admitted device is allowed")

			revoke()
			tc.refresh(fc)
			for i := range 3 {
				requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"),
					fmt.Sprintf("event %d after revocation must be denied", i+1))
			}
			// Past the rejected token's expiry the relay can no longer resend it:
			// it must still not reach the fail-open no-token path.
			fc.now = fc.now.Add(20 * time.Minute)
			requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"),
				"a revoked box stays closed after its token expires")

			// Observe-only events from a revoked box are dropped, not spooled.
			invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")
			t.Setenv("XDG_STATE_HOME", stateHome)
			require.Empty(t, listSpoolEntries(spoolDirPath()), "a refused device's events are not kept for replay")
			for _, h := range fs.headers {
				require.Empty(t, h.Get("Gram-Key"), "a revoked token box never falls back to a key")
			}
		})
	}
}

// The same guarantee for one long-running runner, as serve mode uses.
func TestRevokedTokenBoxNeverFailsOpenInServeMode(t *testing.T) {
	fs, revoke := revocableServer(t)
	cfg, fc := tokenBox(t, fs.URL)
	setSpoolStateHome(t)
	writeOrgSettings(cfg, true)
	runner := NewRunner(cfg)
	payload := agenthookstest.Fixture(t, "claude/pre_tool_use.json")

	res := agenthookstest.Invoke(t, runner, agenthooks.ProviderClaudeCode, payload, "--variant=cli")
	require.Equal(t, "{}", strings.TrimSpace(string(res.Stdout)))

	revoke()
	fc.fail = errors.New("exit status 1")
	for range 3 {
		requireDenied(t, agenthookstest.Invoke(t, runner, agenthooks.ProviderClaudeCode, payload, "--variant=cli"), "serve mode stays closed")
	}
	fc.now = fc.now.Add(20 * time.Minute)
	requireDenied(t, agenthookstest.Invoke(t, runner, agenthooks.ProviderClaudeCode, payload, "--variant=cli"), "serve mode stays closed past expiry")
}

// A device revoked while it sent nothing never sees a 401: its token expires
// and the command refuses. That refusal must also fail closed.
func TestRefusedTokenCommandFailsClosedWithoutPriorRejection(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, fc := tokenBox(t, fs.URL)
	stateHome := setSpoolStateHome(t)
	writeOrgSettings(cfg, true)

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	fc.now = fc.now.Add(20 * time.Minute)
	fc.fail = fmt.Errorf("%w: exit status 3", errTokenRefused)

	requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"), "a refusal is not an outage")
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/post_tool_use.json")
	t.Setenv("XDG_STATE_HOME", stateHome)
	require.Empty(t, listSpoolEntries(spoolDirPath()))
}

// An unreachable control plane is a missing verdict, not a refusal: the box
// keeps following the org's fail-open posture.
func TestUnreachableTokenCommandFollowsFailOpenPosture(t *testing.T) {
	fs := newFakeServer(t, nil)
	cfg, fc := tokenBox(t, fs.URL)
	setSpoolStateHome(t)
	writeOrgSettings(cfg, true)

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	fc.now = fc.now.Add(20 * time.Minute)
	fc.fail = errors.New("exit status 1: control plane unreachable")

	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Equal(t, "{}", strings.TrimSpace(string(res.Stdout)), "an outage follows fail-open")
	require.NoFileExists(t, tokenRejectedPath())
}

// A damaged token cache must not clear a refusal.
func TestRefusalSurvivesACorruptTokenCache(t *testing.T) {
	fs, revoke := revocableServer(t)
	cfg, fc := tokenBox(t, fs.URL)
	setSpoolStateHome(t)
	writeOrgSettings(cfg, true)

	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	revoke()
	fc.fail = errors.New("exit status 1")
	requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"), "rejected")
	require.NoError(t, os.WriteFile(authFilePath(), []byte("\x00garbage"), 0o600))

	requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"), "corruption must not reopen a refused box")
}

// Re-enrolling (a fresh token the server accepts) lifts the refusal.
func TestAcceptedTokenClearsRefusal(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusUnauthorized)
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		if int(status.Load()) == http.StatusOK {
			return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
		}
		return int(status.Load()), decision{Decision: "", Reason: "", Message: "refused"}
	})
	cfg, fc := tokenBox(t, fs.URL)
	setSpoolStateHome(t)

	requireDenied(t, invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json"), "refused")
	require.FileExists(t, tokenRejectedPath())

	status.Store(http.StatusOK)
	fc.now = fc.now.Add(20 * time.Minute)
	res := invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Equal(t, "{}", strings.TrimSpace(string(res.Stdout)))
	require.NoFileExists(t, tokenRejectedPath())
}

func TestExecTokenCommandReportsRefusalExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script")
	}
	dir := t.TempDir()
	refused := filepath.Join(dir, "refused")
	require.NoError(t, os.WriteFile(refused, []byte("#!/bin/sh\necho 'device revoked' >&2\nexit 3\n"), 0o700)) // #nosec G306 -- test script must be executable
	_, err := execTokenCommand(context.Background(), []string{refused}, false)
	require.ErrorIs(t, err, errTokenRefused)

	failing := filepath.Join(dir, "failing")
	require.NoError(t, os.WriteFile(failing, []byte("#!/bin/sh\nexit 1\n"), 0o700)) // #nosec G306 -- test script must be executable
	_, err = execTokenCommand(context.Background(), []string{failing}, false)
	require.Error(t, err)
	require.NotErrorIs(t, err, errTokenRefused)
}
