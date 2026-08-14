package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/chat"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// handoffRoutePrefix is the public serving path. Links are always minted
// against serverURL, so this is served on the primary app domain only — it is
// deliberately absent from the custom-domain ingress allowlist in
// k8s/ingress_provisioner.go, which exists so customer domains can serve skill
// share pages. It must stay in lockstep with the route attached in Attach and
// with the token-redaction prefixes in middleware.logSafeURL.
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
		// Clamp in seconds, before widening to a Duration: converting first
		// lets an absurd request overflow the nanosecond multiplication and
		// wrap past the floor, so a hostile -1e10 would mint an hour-long link
		// instead of the one-minute minimum.
		seconds := min(max(*payload.TTLSeconds, int(minHandoffTTL/time.Second)), int(maxHandoffTTL/time.Second))
		ttl = time.Duration(seconds) * time.Second
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

	// The document goes to object storage, never the row: the row keeps the
	// capability token, tenancy, and the atomic consume claim. Written before
	// the transaction so a failed insert can only leak an unreferenced blob
	// (cleaned up below; bucket lifecycle is the backstop) — never a live
	// capability with no document behind it.
	blobW, blobURL, err := s.blobStore.Write(ctx, "session-handoffs/"+token+".md", "text/markdown", int64(len(payload.Content)))
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "store handoff document").LogError(ctx, s.logger)
	}
	if _, err := io.WriteString(blobW, payload.Content); err != nil {
		_ = blobW.Close()
		return nil, oops.E(oops.CodeUnexpected, err, "store handoff document").LogError(ctx, s.logger)
	}
	if err := blobW.Close(); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "store handoff document").LogError(ctx, s.logger)
	}
	// The blob URL is a second capability: it must reach the database and
	// nothing else — never the response, the audit trail, or a log line.
	cleanupBlob := func() {
		if err := s.blobStore.Delete(context.WithoutCancel(ctx), blobURL); err != nil {
			s.logger.WarnContext(ctx, "orphaned handoff blob not deleted", attr.SlogError(err))
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		cleanupBlob()
		return nil, oops.E(oops.CodeUnexpected, err, "store handoff").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	row, err := repo.New(dbtx).InsertSessionHandoffLink(ctx, repo.InsertSessionHandoffLinkParams{
		ProjectID:      *authCtx.ProjectID,
		OrganizationID: authCtx.ActiveOrganizationID,
		SessionID:      sessionID,
		Token:          token,
		BlobUrl:        blobURL.String(),
		CreatedByEmail: createdBy,
		ExpiresAt:      conv.ToPGTimestamptz(expiresAt),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// The insert selects through the tenant-qualified project, so no row
		// means the key's project and organization disagree or the project is
		// soft-deleted. Unreachable from a well-formed auth context.
		cleanupBlob()
		return nil, oops.E(oops.CodeNotFound, err, "project not found")
	case err != nil:
		cleanupBlob()
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
		// The deferred rollback discards the row, so the blob would outlive
		// every reference to it.
		cleanupBlob()
		return nil, oops.E(oops.CodeUnexpected, err, "record handoff export").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		// Deliberately no cleanup here: an errored commit may still have
		// landed server-side, and deleting the blob under a row that did
		// commit would leave a live link serving nothing. An orphaned blob is
		// the better failure, and bucket lifecycle collects it.
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
	// Shape check before the lookup: length first, so an attacker-sized path
	// segment is rejected before hex.DecodeString allocates for it, then the
	// now-fixed-size decode. Only hex of exactly the minted length can name a
	// link, so malformed probes never reach Postgres.
	if len(token) != hex.EncodedLen(handoffTokenBytes) {
		http.NotFound(w, r)
		return nil
	}
	if _, err := hex.DecodeString(token); err != nil {
		http.NotFound(w, r)
		return nil
	}

	rawBlobURL, err := s.repo.ConsumeSessionHandoffLink(r.Context(), token)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		http.NotFound(w, r)
		return nil
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "resolve handoff link").LogError(r.Context(), s.logger)
	}

	// The claim is already burned in Postgres; everything past this point is
	// delivery. The blob URL is internal — it must never surface in the
	// response or an error body, so failures log it out-of-band and the
	// caller sees a generic error.
	blobURL, err := url.Parse(rawBlobURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve handoff document").LogError(r.Context(), s.logger)
	}
	rdr, err := s.blobStore.Read(r.Context(), blobURL)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "read handoff document").LogError(r.Context(), s.logger)
	}
	defer o11y.LogDefer(r.Context(), s.logger, func() error { return rdr.Close() })

	// no-store on a single-use document: any cache replaying it would defeat
	// burn-after-read; noindex belt-and-braces against crawler ingestion.
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, rdr); err != nil {
		s.logger.DebugContext(r.Context(), "write handoff response", attr.SlogError(err))
	}

	// Burn the document itself, best-effort: the row's claim already makes the
	// link dead, so a failed delete only means the bucket lifecycle policy
	// collects the orphan later.
	if err := s.blobStore.Delete(context.WithoutCancel(r.Context()), blobURL); err != nil {
		s.logger.WarnContext(r.Context(), "consumed handoff blob not deleted", attr.SlogError(err))
	}
	return nil
}
