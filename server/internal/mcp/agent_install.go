// One-command install for an agent gateway.
//
// The dashboard shows an agent's key exactly once, so a copyable install
// command must not contain it: a one-liner lands in shell history, in chat
// threads, and in screen shares. Instead the dashboard exchanges the key it is
// already holding for a short-lived, single-use code, and the command carries
// the code. Fetching the script consumes the code and returns a script with
// the key inlined, so a leaked command is worthless a moment later.

package mcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
)

// AgentInstallCodeRoute mints a code; AgentInstallScriptRoute spends it.
const (
	AgentInstallCodeRoute   = "/agent-mcp/{agentID}/install-code"
	AgentInstallScriptRoute = "/agent-mcp/install/{code}"
)

// agentInstallCodeTTL is short because the code is meant to be used by the
// person who just created the key, in the command they were shown.
const agentInstallCodeTTL = 15 * time.Minute

// maxAgentInstallRequestBytes bounds the install-code request body. It carries
// two short enum values, so 1 KiB is generous.
const maxAgentInstallRequestBytes = 1 << 10 // 1 KiB

// agentInstallFlavor selects which script a code exchanges for. An agent key
// is issued for exactly one of them, never both.
type agentInstallFlavor string

const (
	// agentInstallFlavorMCP configures local MCP clients with the agent gateway.
	// It is the default, so a request without a body keeps that behavior.
	agentInstallFlavorMCP agentInstallFlavor = "mcp"

	// agentInstallFlavorDeviceAgent installs the device agent and enrolls it
	// under the agent's identity.
	agentInstallFlavorDeviceAgent agentInstallFlavor = "device_agent"
)

// agentInstallRequest is the optional JSON body of an install-code request.
type agentInstallRequest struct {
	// Flavor picks the script; empty means agentInstallFlavorMCP.
	Flavor agentInstallFlavor `json:"flavor"`

	// Mode is how the device agent runs. Required for the device agent flavor
	// and rejected for any other.
	Mode deviceAgentRunMode `json:"mode"`
}

// agentInstallCode is the exchange record. It holds the credential, so it
// lives only in the cache, only for its TTL, and is deleted on first read.
type agentInstallCode struct {
	// Code is the single-use value the install command carries.
	Code string `json:"code"`

	// AgentID is the agent the key belongs to.
	AgentID string `json:"agent_id"`

	// Key is the live agent key the script inlines.
	Key string `json:"key"`

	// URL is the agent gateway for the MCP flavor, and the control plane the
	// device agent reports to for the device agent flavor.
	URL string `json:"url"`

	// Flavor is the script this code renders. Empty reads as MCP.
	Flavor agentInstallFlavor `json:"flavor,omitempty"`

	// Mode is the device agent's run mode; empty for the MCP flavor.
	Mode deviceAgentRunMode `json:"mode,omitempty"`
}

var _ cache.CacheableObject[agentInstallCode] = (*agentInstallCode)(nil)

func (a agentInstallCode) CacheKey() string   { return "agentInstall:" + a.Code }
func (a agentInstallCode) TTL() time.Duration { return agentInstallCodeTTL }

// newInstallCode returns a URL-safe code with 256 bits of entropy. It is a
// bearer of the agent key for its lifetime, so it is sized like one.
func newInstallCode() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate install code: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HandleAgentInstallCode exchanges a live agent key for a single-use install
// code. Authenticated by that key, so possession of the key is the only thing
// that can mint one, and only for the agent the key belongs to.
func (s *Service) HandleAgentInstallCode(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	agentID, err := uuid.Parse(chi.URLParam(r, "agentID"))
	if err != nil || agentID == uuid.Nil {
		return oops.C(oops.CodeNotFound)
	}
	keyCtx, authCtx, err := s.authenticateAgentGatewayKey(ctx, r, agentID)
	if err != nil {
		return err
	}

	request, err := decodeAgentInstallRequest(r)
	if err != nil {
		return oops.E(oops.CodeBadRequest, err, "invalid install request").LogWarn(ctx, s.logger)
	}

	target := s.BaseURLForRequest(r) + "/agent-mcp/" + agentID.String()
	switch request.Flavor {
	case agentInstallFlavorMCP:
	case agentInstallFlavorDeviceAgent:
		target, err = s.deviceAgentInstallTarget(keyCtx, authCtx, request.Mode)
		if err != nil {
			return err
		}
	default:
		return oops.E(oops.CodeBadRequest, nil, "unknown install flavor %q", request.Flavor).LogWarn(ctx, s.logger)
	}

	code, err := newInstallCode()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "prepare install command").LogError(ctx, s.logger)
	}
	record := agentInstallCode{
		Code:    code,
		AgentID: agentID.String(),
		Key:     httpheaders.AuthorizationBearerToken(r),
		URL:     target,
		Flavor:  request.Flavor,
		Mode:    request.Mode,
	}
	if err := s.agentInstallCache.Store(keyCtx, record); err != nil {
		return oops.E(oops.CodeUnexpected, err, "store install command").LogError(ctx, s.logger)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if _, err := fmt.Fprintf(w, `{"code":%q,"expires_in_seconds":%d}`, code, int(agentInstallCodeTTL.Seconds())); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write install command").LogError(ctx, s.logger)
	}
	return nil
}

// HandleAgentInstallScript spends a code and returns the shell script that
// configures the local MCP clients. Unauthenticated by design: the code is the
// credential, which is why it is single-use and short-lived.
func (s *Service) HandleAgentInstallScript(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	code := chi.URLParam(r, "code")
	if code == "" {
		return oops.C(oops.CodeNotFound)
	}
	// GetAndDelete, not Get: a code that has been read is spent, whether or not
	// the script that follows reaches its target.
	// Only the key matters for the lookup; the rest comes back from the cache.
	lookup := agentInstallCode{Code: code, AgentID: "", Key: "", URL: "", Flavor: "", Mode: ""}
	record, err := s.agentInstallCache.GetAndDelete(ctx, lookup.CacheKey())
	if err != nil || record.Key == "" {
		// An expired, unknown or already-spent code are the same thing to the
		// caller: nothing to hand over.
		return oops.C(oops.CodeNotFound)
	}

	var script string
	switch record.Flavor {
	case agentInstallFlavorDeviceAgent:
		script, err = deviceAgentInstallScript(record.URL, record.Key, record.Mode)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "render install script").LogError(ctx, s.logger)
		}
	case agentInstallFlavorMCP, "":
		script = agentInstallScript(record.URL, record.Key)
	default:
		return oops.C(oops.CodeNotFound)
	}

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	// The body is a credential. Nothing may keep a copy.
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write([]byte(script)); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write install script").LogError(ctx, s.logger)
	}
	return nil
}

// decodeAgentInstallRequest reads the optional request body. No body is the
// MCP flavor, which is what the dashboard sent before flavors existed.
func decodeAgentInstallRequest(r *http.Request) (agentInstallRequest, error) {
	request := agentInstallRequest{Flavor: agentInstallFlavorMCP, Mode: ""}
	if r.Body == nil {
		return request, nil
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxAgentInstallRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		return request, fmt.Errorf("decode install request: %w", err)
	}
	// One JSON value and nothing after it: a second value or trailing bytes
	// mean the body is not what the caller thinks it sent.
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return request, errors.New("install request has trailing data")
	}
	if request.Flavor == "" {
		request.Flavor = agentInstallFlavorMCP
	}
	if request.Flavor != agentInstallFlavorDeviceAgent && request.Mode != "" {
		return request, fmt.Errorf("mode applies only to the %s flavor", agentInstallFlavorDeviceAgent)
	}
	return request, nil
}

// deviceAgentInstallTarget checks that this key may install the device agent
// and returns the control plane it will report to.
//
// The key must be able to sync, and must not reach MCP servers: an agent key
// is issued for one runtime, and a device agent key written to disk on a
// shared host must not double as a gateway credential.
func (s *Service) deviceAgentInstallTarget(ctx context.Context, authCtx *contextvalues.AuthContext, mode deviceAgentRunMode) (string, error) {
	if !slices.Contains(deviceAgentRunModes, mode) {
		return "", oops.E(oops.CodeBadRequest, nil, "mode must be %q or %q", deviceAgentRunModeEphemeral, deviceAgentRunModeService).LogWarn(ctx, s.logger)
	}

	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgDeviceAgentSync, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return "", fmt.Errorf("authorize device agent install: %w", err)
	}

	credential, ok := contextvalues.PrincipalCredentialAuthorization(ctx)
	if !ok {
		return "", oops.C(oops.CodeUnauthorized)
	}
	policy, err := runtimepolicy.DecodeDelegatedPolicy(runtimepolicy.DelegatedPolicyVersion(credential.DelegatedGrantsVersion), credential.DelegatedGrants)
	if err != nil {
		return "", oops.E(oops.CodeUnexpected, err, "read key policy").LogError(ctx, s.logger)
	}
	for _, grant := range policy.RuntimeGrants() {
		if grant.Scope == authz.ScopeMCPConnect {
			return "", oops.E(oops.CodeForbidden, nil, "This key can connect to MCP servers, so it cannot install the device agent. Issue the agent a device agent key.").LogWarn(ctx, s.logger)
		}
	}

	// The device agent calls the sync and hooks APIs, which only the platform
	// hosts serve; a custom domain or private ingress falls back to the server.
	controlPlane := requestorigin.PlatformHostBaseURL(ctx, s.serverURL.String(), s.serverURL.String())
	parsed, err := url.Parse(controlPlane)
	// No loopback exception: the script runs on the agent's host, where
	// localhost is that machine and not this server.
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", oops.E(oops.CodeFailedPrecondition, err, "This deployment is not served over HTTPS, so the device agent key would travel in plaintext.").LogWarn(ctx, s.logger)
	}
	return strings.TrimRight(controlPlane, "/"), nil
}

// shellSingleQuote renders a value as a POSIX single-quoted word. Both the URL
// and the key are interpolated into a script that runs on someone's machine,
// so neither may be allowed to end the quoting and start a command.
func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// agentInstallScript writes the gateway into whichever MCP clients are
// installed. It configures what it finds and says what it did; it never
// installs a client, and it is idempotent, so re-running after a grant change
// is safe.
func agentInstallScript(url, key string) string {
	return `#!/bin/sh
# Connects this machine's MCP clients to a Gram agent gateway.
# Generated for one use. The key below is live: treat this file as a secret.
set -eu

GRAM_AGENT_URL=` + shellSingleQuote(url) + `
GRAM_AGENT_KEY=` + shellSingleQuote(key) + `
export GRAM_AGENT_KEY

configured=0

if command -v claude >/dev/null 2>&1; then
  # Idempotent: remove any previous entry before adding this one.
  claude mcp remove gram >/dev/null 2>&1 || true
  claude mcp add --transport http gram "$GRAM_AGENT_URL" \
    --header "Authorization: Bearer $GRAM_AGENT_KEY"
  echo "Configured Claude Code."
  configured=$((configured + 1))
fi

if command -v codex >/dev/null 2>&1; then
  codex mcp remove gram >/dev/null 2>&1 || true
  codex mcp add gram --url "$GRAM_AGENT_URL" \
    --bearer-token-env-var GRAM_AGENT_KEY
  echo "Configured Codex."
  configured=$((configured + 1))
fi

if [ "$configured" -eq 0 ]; then
  echo "No supported MCP client found on PATH."
  echo "Connect one manually with:"
  echo "  URL:    $GRAM_AGENT_URL"
  echo "  Header: Authorization: Bearer \$GRAM_AGENT_KEY"
  echo
  echo "GRAM_AGENT_KEY is set in this shell only. Store it before closing."
  exit 1
fi

echo
echo "Done. GRAM_AGENT_KEY was exported for this run only — store it if a"
echo "client you use reads it from the environment."
`
}
