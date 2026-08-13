package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// handoffRoutePrefix is the public serving path. It must stay in lockstep
// with the ingress allowlist (k8s/ingress_provisioner.go) and the route
// attached in Attach.
const handoffRoutePrefix = "/shared/handoffs/"

const (
	// maxHandoffContentBytes caps the uploaded document in BYTES. The design's
	// MaxLength counts runes, so a multi-byte document could pass Goa
	// validation while exceeding the intended storage bound without this.
	maxHandoffContentBytes = 262144

	// TTL bounds. The default keeps a leaked link's window to minutes; the
	// max exists so a caller cannot mint an effectively permanent link.
	minHandoffTTL     = time.Minute
	defaultHandoffTTL = 15 * time.Minute
	maxHandoffTTL     = time.Hour

	// handoffTokenBytes of entropy, hex-encoded, make the token the
	// capability: 32 bytes is far beyond guessable at any request rate.
	handoffTokenBytes = 32
)

// CreateSessionHandoff mints a burn-after-read capability URL for an uploaded
// handoff document. This is the one place session CONTENT transits the server
// in the local-first design, so it takes the strictest identity posture
// (per-user key only, like GetSessionMeta) and records a content-free
// chat_session:handoff_export audit entry in the same transaction as the
// link insert.
func (s *Service) CreateSessionHandoff(ctx context.Context, payload *gen.CreateSessionHandoffPayload) (*gen.CreateSessionHandoffResult, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if slices.Contains(authCtx.APIKeyScopes, auth.APIKeyScopeAgent.String()) {
		return nil, oops.E(oops.CodeForbidden, nil, "minting a handoff link requires a per-user agent key; the org install key cannot upload content")
	}
	if authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if err := s.requireSessionPortability(ctx, authCtx.ActiveOrganizationID); err != nil {
		return nil, err
	}

	sessionID := strings.TrimSpace(payload.SessionID)
	if sessionID == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "session_id is required")
	}
	if strings.TrimSpace(payload.Content) == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "content is required")
	}
	if len(payload.Content) > maxHandoffContentBytes {
		return nil, oops.E(oops.CodeBadRequest, nil, "content exceeds the handoff size limit")
	}

	ttl := defaultHandoffTTL
	if payload.TTLSeconds != nil {
		ttl = min(max(time.Duration(*payload.TTLSeconds)*time.Second, minHandoffTTL), maxHandoffTTL)
	}

	tokenBytes := make([]byte, handoffTokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "mint handoff token").LogError(ctx, s.logger)
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(ttl)

	createdBy := conv.PtrValOr(authCtx.Email, "")
	if createdBy == "" {
		createdBy = authCtx.UserID
	}

	chatID := chat.SessionIDToChatID(sessionID)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "store handoff").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).InsertSessionHandoffLink(ctx, repo.InsertSessionHandoffLinkParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		SessionID:      sessionID,
		Token:          token,
		Content:        payload.Content,
		CreatedByEmail: createdBy,
		ExpiresAt:      conv.ToPGTimestamptz(expiresAt),
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "store handoff").LogError(ctx, s.logger)
	}

	event := audit.LogChatSessionHandoffExportEvent{
		OrganizationID:   authCtx.ActiveOrganizationID,
		ProjectID:        *authCtx.ProjectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		ChatSessionURN:   urn.NewChatSession(chatID),
		ChatTitle:        "",
		SourceSurface:    strings.TrimSpace(conv.PtrValOr(payload.SourceSurface, "")),
		ContentBytes:     len(payload.Content),
		TTLSeconds:       int(ttl / time.Second),
		ExpiresAt:        expiresAt,
		DeviceSerial:     normalizeSerial(payload.SerialNumber),
		DeviceHostname:   strings.TrimSpace(conv.PtrValOr(payload.Hostname, "")),
	}
	if err := s.audit.LogChatSessionHandoffExport(ctx, dbtx, event); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record handoff export").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "store handoff").LogError(ctx, s.logger)
	}

	return &gen.CreateSessionHandoffResult{
		URL:       strings.TrimRight(s.serverURL, "/") + handoffRoutePrefix + token,
		ExpiresAt: row.ExpiresAt.Time.UTC().Format(time.RFC3339),
	}, nil
}

// ServeSessionHandoff serves a minted handoff document at
// GET /shared/handoffs/{token}. Unauthenticated by design — the unguessable
// token is the credential — and single-shot: the consume query atomically
// claims the row, so expired, already-read, and never-existed tokens are an
// indistinguishable 404 with no timing or body difference for a prober.
func (s *Service) ServeSessionHandoff(w http.ResponseWriter, r *http.Request) error {
	token := chi.URLParam(r, "token")
	if len(token) != 2*handoffTokenBytes {
		http.NotFound(w, r)
		return nil
	}

	content, err := s.repo.ConsumeSessionHandoffLink(r.Context(), token)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "resolve handoff link").LogError(r.Context(), s.logger)
	}

	// no-store on a single-use document: any cache replaying it would defeat
	// burn-after-read; noindex belt-and-braces against crawler ingestion.
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := w.Write([]byte(content)); err != nil {
		s.logger.DebugContext(r.Context(), "write handoff response", attr.SlogError(err))
	}
	return nil
}
