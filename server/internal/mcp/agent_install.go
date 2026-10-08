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
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// AgentInstallCodeRoute mints a code; AgentInstallScriptRoute spends it.
const (
	AgentInstallCodeRoute   = "/agent-mcp/{agentID}/install-code"
	AgentInstallScriptRoute = "/agent-mcp/install/{code}"
)

// agentInstallCodeTTL is short because the code is meant to be used by the
// person who just created the key, in the command they were shown.
const agentInstallCodeTTL = 15 * time.Minute

// agentInstallCode is the exchange record. It holds the credential, so it
// lives only in the cache, only for its TTL, and is deleted on first read.
type agentInstallCode struct {
	Code    string `json:"code"`
	AgentID string `json:"agent_id"`
	Key     string `json:"key"`
	URL     string `json:"url"`
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
	keyCtx, _, err := s.authenticateAgentGatewayKey(ctx, r, agentID)
	if err != nil {
		return err
	}

	code, err := newInstallCode()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "prepare install command").LogError(ctx, s.logger)
	}
	record := agentInstallCode{
		Code:    code,
		AgentID: agentID.String(),
		Key:     httpheaders.AuthorizationBearerToken(r),
		URL:     s.BaseURLForRequest(r) + "/agent-mcp/" + agentID.String(),
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
	lookup := agentInstallCode{Code: code, AgentID: "", Key: "", URL: ""}
	record, err := s.agentInstallCache.GetAndDelete(ctx, lookup.CacheKey())
	if err != nil || record.Key == "" {
		// An expired, unknown or already-spent code are the same thing to the
		// caller: nothing to hand over.
		return oops.C(oops.CodeNotFound)
	}

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	// The body is a credential. Nothing may keep a copy.
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write([]byte(agentInstallScript(record.URL, record.Key))); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write install script").LogError(ctx, s.logger)
	}
	return nil
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
