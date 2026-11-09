package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/agent"
	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/deviceidentity"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sessionhandoff"
)

// maxHandoffContentBytes caps uploaded documents in bytes.
const maxHandoffContentBytes = 262144

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

	ttlSeconds := payload.TTLSeconds
	ttl := sessionhandoff.ClampHandoffTTL(ttlSeconds)
	sourceSurface := strings.TrimSpace(conv.PtrValOr(payload.SourceSurface, ""))
	deviceSerial := deviceidentity.NormalizeSerial(payload.SerialNumber)
	deviceHostname := strings.TrimSpace(conv.PtrValOr(payload.Hostname, ""))
	idempotencyKey := strings.TrimSpace(conv.PtrValOr(payload.IdempotencyKey, ""))
	if len(idempotencyKey) > sessionhandoff.MaxIdempotencyKeyBytes {
		return nil, oops.E(oops.CodeBadRequest, nil, "idempotency_key exceeds 128 bytes")
	}
	idempotencyHash := ""
	if idempotencyKey != "" {
		idempotencyHash = sessionhandoff.DeviceIdempotencyInputHash(sessionID, payload.Content, sourceSurface, deviceSerial, deviceHostname, ttl)
	}
	result, err := sessionhandoff.NewSessionHandoffMinter(s.logger, s.db, s.blobStore, s.serverURL, s.audit).Mint(ctx, sessionhandoff.SessionHandoffMintInput{
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            *authCtx.ProjectID,
		UserID:               authCtx.UserID,
		ActorEmail:           authCtx.Email,
		ChatID:               chat.SessionIDToChatID(sessionID),
		SessionID:            sessionID,
		ChatTitle:            "",
		Content:              payload.Content,
		TTLSeconds:           ttlSeconds,
		SourceSurface:        sourceSurface,
		DeviceSerial:         deviceSerial,
		DeviceHostname:       deviceHostname,
		IdempotencyOperation: "device_create",
		IdempotencyKey:       idempotencyKey,
		IdempotencyInputHash: idempotencyHash,
	})
	if err != nil {
		return nil, fmt.Errorf("mint session handoff: %w", err)
	}
	return &gen.CreateSessionHandoffResult{URL: result.URL, ExpiresAt: result.ExpiresAt}, nil
}

// ServeSessionHandoff serves a minted handoff document at
// GET /shared/handoffs/{token}. Unauthenticated by design — the unguessable
// token is the credential — and single-shot: the consume query atomically
// claims the row, so expired, already-read, and never-existed tokens are an
// indistinguishable 404 with no timing or body difference for a prober.
func (s *Service) ServeSessionHandoff(w http.ResponseWriter, r *http.Request) error {
	// Set the security posture before inspecting the bearer token so malformed,
	// expired, and successful responses all carry the same protections.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	token := chi.URLParam(r, "token")
	// Shape check before the lookup: length first, so an attacker-sized path
	// segment is rejected before hex.DecodeString allocates for it, then the
	// now-fixed-size decode. Only hex of exactly the minted length can name a
	// link, so malformed probes never reach Postgres.
	if !sessionhandoff.ValidToken(token) {
		http.NotFound(w, r)
		return nil
	}

	claim, err := s.repo.ConsumeSessionHandoffLink(r.Context(), token)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "resolve handoff link").LogError(r.Context(), s.logger)
	}

	// The claim has burned the capability and leased the blob to this consumer;
	// the pointer remains durable until deletion succeeds. The blob URL is
	// internal — it must never surface in the
	// response or an error body, so failures log it out-of-band and the
	// caller sees a generic error.
	blobURL, err := url.Parse(claim.BlobUrl)
	if err != nil {
		// The durable pointer remains claimed. After the consumer lease expires,
		// cleanup will quarantine the malformed row without blocking later work.
		return oops.E(oops.CodeUnexpected, err, "resolve handoff document").LogError(r.Context(), s.logger)
	}

	// The consumer lease is the sole deletion owner while this handler runs.
	// Deferred deletion covers read and response failures; if deletion fails,
	// the pointer and registry remain durable for cleanup after the lease.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 10*time.Second)
		defer cancel()
		if err := s.blobStore.Delete(cleanupCtx, blobURL); err != nil {
			s.logger.WarnContext(r.Context(), "consumed handoff blob not deleted", attr.SlogError(err))
			return
		}
		if _, err := s.repo.FinalizeConsumedSessionHandoffBlob(cleanupCtx, repo.FinalizeConsumedSessionHandoffBlobParams(claim)); err != nil {
			s.logger.WarnContext(r.Context(), "consumed handoff blob state not finalized", attr.SlogError(err))
			return
		}
		if _, err := s.repo.DeleteSessionHandoffUploadRegistryByBlob(cleanupCtx, repo.DeleteSessionHandoffUploadRegistryByBlobParams{ProjectID: claim.ProjectID, BlobUrl: claim.BlobUrl}); err != nil {
			s.logger.WarnContext(r.Context(), "consumed handoff blob registry not cleared", attr.SlogError(err))
		}
	}()

	rdr, err := s.blobStore.Read(r.Context(), blobURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "read handoff document").LogError(r.Context(), s.logger)
	}
	defer o11y.LogDefer(r.Context(), s.logger, "failed to close handoff document reader", func() error { return rdr.Close() })

	// Force a download with a fixed filename. The CSP is intentionally hostile
	// to active content in case a browser ignores the attachment disposition.
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="session-handoff.md"`)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if _, err := io.Copy(w, rdr); err != nil {
		s.logger.DebugContext(r.Context(), "write handoff response", attr.SlogError(err))
	}
	return nil
}
