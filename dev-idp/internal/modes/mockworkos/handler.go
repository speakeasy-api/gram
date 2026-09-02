// Package mockworkos implements the dev-idp's mock-workos mode — a mock
// WorkOS REST surface backed by the dev-idp's shared SQLite store.
//
// Wire-shape compatibility with the workos-go SDK is preserved so
// Gram-side's `*workos.Client` can swap api.workos.com for this listener
// with no code changes.
package mockworkos

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// Mode is the discriminator persisted on rows owned by this handler.
const Mode = "mock-workos"

// Prefix is the URL prefix the dev-idp listener mounts this handler under.
const Prefix = "/mock-workos"

// passwordlessState holds the in-memory state for a passwordless magic-link
// session. Ephemeral — only needs to survive long enough for the local-dev
// user to click the link and complete the code exchange.
type passwordlessState struct {
	email       string
	redirectURI string
	state       string
	code        string
	expiresAt   time.Time
}

// Handler serves the mock-workos mode's HTTP routes.
// Config carries the settings the mock needs from the dev-idp process.
type Config struct {
	// ExternalURL is the dev-idp's externally reachable base URL (no
	// trailing slash). Links handed back to the dashboard, such as the mock
	// admin portal, are built on it so they reach this dev-idp instance
	// even when several worktrees run on remapped ports.
	ExternalURL string
}

type Handler struct {
	cfg    Config
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB

	pwlMu       sync.Mutex
	pwlSessions map[string]*passwordlessState // keyed by session ID

	// Admin portal setups completed through the mock portal page. SSO
	// connections and directories only exist for an organization once the
	// matching intent has been completed, so a fresh dev-idp reports nothing
	// configured until someone clicks through the portal. Restarting dev-idp
	// resets it.
	portalMu        sync.Mutex
	portalCompleted map[portalSetup]bool
}

// portalSetup identifies one admin portal flow: an organization plus the
// WorkOS intent ("sso" or "dsync") it was opened with.
type portalSetup struct {
	organization string
	intent       string
}

func NewHandler(cfg Config, logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *Handler {
	return &Handler{
		cfg:         cfg,
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/modes/mockworkos"),
		logger:      logger.With(slog.String("component", "devidp."+Mode)),
		db:          db,
		pwlSessions: make(map[string]*passwordlessState),

		portalCompleted: make(map[portalSetup]bool),
	}
}

func (h *Handler) markPortalCompleted(organization, intent string) {
	h.portalMu.Lock()
	defer h.portalMu.Unlock()
	h.portalCompleted[portalSetup{organization: organization, intent: intent}] = true
}

func (h *Handler) isPortalCompleted(organization, intent string) bool {
	h.portalMu.Lock()
	defer h.portalMu.Unlock()
	return h.portalCompleted[portalSetup{organization: organization, intent: intent}]
}

// Handler returns the http.Handler that should be mounted under
// `Prefix` (use http.StripPrefix). All registered paths are relative to
// that prefix.
func (h *Handler) Handler() http.Handler {
	mux := http.NewServeMux()
	h.registerWorkosRoutes(mux)
	return mux
}

// =============================================================================
// Shared helpers used by workos.go
// =============================================================================

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func randomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
