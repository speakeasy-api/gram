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
type Handler struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *sql.DB

	pwlMu       sync.Mutex
	pwlSessions map[string]*passwordlessState // keyed by session ID
}

func NewHandler(logger *slog.Logger, tracerProvider trace.TracerProvider, db *sql.DB) *Handler {
	return &Handler{
		tracer:      tracerProvider.Tracer("github.com/speakeasy-api/gram/dev-idp/internal/modes/mockworkos"),
		logger:      logger.With(slog.String("component", "devidp."+Mode)),
		db:          db,
		pwlSessions: make(map[string]*passwordlessState),
	}
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
