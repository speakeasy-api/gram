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
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/mcp/agentinstall"
	"github.com/speakeasy-api/gram/server/internal/mcp/httpheaders"
	"github.com/speakeasy-api/gram/server/internal/oops"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
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

	// ServerURL is the origin the code was minted on, which the OpenClaw
	// hooks plugin reports to.
	ServerURL string `json:"server_url"`

	// OrganizationID is the agent's organization.
	OrganizationID string `json:"organization_id"`

	// ProjectSlug is the project the agent's hooks are recorded in, empty
	// when the agent belongs to no project.
	ProjectSlug string `json:"project_slug"`

	// InstallFailOpen is the organization's hooks install-failure policy when
	// the code was minted.
	InstallFailOpen bool `json:"install_fail_open"`
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
	organizationID := authCtx.ActiveOrganizationID

	// Resolved now, while the request is authenticated by the key: spending
	// the code is not.
	projectSlug, err := s.agentHooksProjectSlug(ctx, organizationID, agentID)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "prepare install command").LogError(ctx, s.logger)
	}

	code, err := newInstallCode()
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "prepare install command").LogError(ctx, s.logger)
	}
	serverURL := s.BaseURLForRequest(r)
	record := agentInstallCode{
		Code:            code,
		AgentID:         agentID.String(),
		Key:             httpheaders.AuthorizationBearerToken(r),
		URL:             serverURL + "/agent-mcp/" + agentID.String(),
		ServerURL:       serverURL,
		OrganizationID:  organizationID,
		ProjectSlug:     projectSlug,
		InstallFailOpen: s.hooksInstallFailOpen(ctx, organizationID),
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

// agentHooksProjectSlug returns the slug of the project the agent's hooks are
// recorded in, or "" when the agent belongs to no project.
func (s *Service) agentHooksProjectSlug(ctx context.Context, organizationID string, agentID uuid.UUID) (string, error) {
	agent, err := agentsrepo.New(s.db).GetAgentByID(ctx, agentsrepo.GetAgentByIDParams{OrganizationID: organizationID, ID: agentID})
	if err != nil {
		return "", fmt.Errorf("get agent: %w", err)
	}
	if !agent.ProjectID.Valid {
		return "", nil
	}

	project, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{
		ID:             agent.ProjectID.UUID,
		OrganizationID: organizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get agent project: %w", err)
	}
	return project.Slug, nil
}

// hooksInstallFailOpen reads the organization's hooks install-failure policy,
// which every generated observability plugin bakes in. An unreadable flag
// keeps the fail-closed default, as the plugins page does.
func (s *Service) hooksInstallFailOpen(ctx context.Context, organizationID string) bool {
	enabled, err := pluginsrepo.New(s.db).IsOrganizationFeatureEnabled(ctx, pluginsrepo.IsOrganizationFeatureEnabledParams{
		OrganizationID: organizationID,
		FeatureName:    string(productfeatures.FeatureHooksFailOpen),
	})
	if err != nil {
		s.logger.WarnContext(ctx, "read hooks fail-open flag for agent install, defaulting to fail closed",
			attr.SlogOrganizationID(organizationID),
			attr.SlogError(err),
		)
		return false
	}
	return enabled
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
	lookup := agentInstallCode{Code: code, AgentID: "", Key: "", URL: "", ServerURL: "", OrganizationID: "", ProjectSlug: "", InstallFailOpen: false}
	record, err := s.agentInstallCache.GetAndDelete(ctx, lookup.CacheKey())
	if err != nil || record.Key == "" {
		// An expired, unknown or already-spent code are the same thing to the
		// caller: nothing to hand over.
		return oops.C(oops.CodeNotFound)
	}

	script, err := agentinstall.Script(agentinstall.Params{
		GatewayURL:      record.URL,
		Key:             record.Key,
		ServerURL:       record.ServerURL,
		OrganizationID:  record.OrganizationID,
		ProjectSlug:     record.ProjectSlug,
		InstallFailOpen: record.InstallFailOpen,
	})
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "render install script").LogError(ctx, s.logger)
	}

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	// The body is a credential. Nothing may keep a copy.
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write([]byte(script)); err != nil {
		return oops.E(oops.CodeUnexpected, err, "write install script").LogError(ctx, s.logger)
	}
	return nil
}
