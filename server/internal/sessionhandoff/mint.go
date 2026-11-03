package sessionhandoff

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/assets"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	MinHandoffTTL     = time.Minute
	DefaultHandoffTTL = 15 * time.Minute
	MaxHandoffTTL     = time.Hour
)

type SessionHandoffMintInput struct {
	OrganizationID string
	ProjectID      uuid.UUID
	UserID         string
	ActorEmail     *string
	ChatID         uuid.UUID
	SessionID      string
	ChatTitle      string
	Content        string
	TTLSeconds     *int
	SourceSurface  string
	DeviceSerial   string
	DeviceHostname string
}

type SessionHandoffMintResult struct {
	URL       string
	ExpiresAt string
}

// SessionHandoffMinter owns the transport-independent token, blob, database,
// and audit transaction used by both device agents and Platform MCP.
type SessionHandoffMinter struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	blobStore assets.BlobStore
	serverURL string
	audit     *audit.Logger
}

func NewSessionHandoffMinter(logger *slog.Logger, db *pgxpool.Pool, blobStore assets.BlobStore, serverURL string, auditor *audit.Logger) *SessionHandoffMinter {
	return &SessionHandoffMinter{logger: logger, db: db, blobStore: blobStore, serverURL: serverURL, audit: auditor}
}

func (m *SessionHandoffMinter) Valid() bool {
	if m == nil || m.logger == nil || m.db == nil || m.blobStore == nil || m.audit == nil {
		return false
	}
	_, ok := validHTTPSBaseURL(m.serverURL)
	return ok
}

func validHTTPSBaseURL(raw string) (*url.URL, bool) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, false
	}
	return parsed, true
}

func publicHandoffURL(baseURL *url.URL, token string) string {
	// validHTTPSBaseURL excludes query, fragment, and userinfo, so appending to
	// the serialized base preserves its configured path without moving the
	// capability token into another URL component.
	return strings.TrimRight(baseURL.String(), "/") + PublicRoutePrefix + token
}

func ClampHandoffTTL(seconds *int) time.Duration {
	if seconds == nil {
		return DefaultHandoffTTL
	}
	return time.Duration(min(max(*seconds, int(MinHandoffTTL/time.Second)), int(MaxHandoffTTL/time.Second))) * time.Second
}

func (m *SessionHandoffMinter) Mint(ctx context.Context, input SessionHandoffMintInput) (SessionHandoffMintResult, error) {
	if !m.Valid() {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, nil, "session handoff unavailable")
	}
	ttl := ClampHandoffTTL(input.TTLSeconds)
	baseURL, ok := validHTTPSBaseURL(m.serverURL)
	if !ok {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, nil, "session handoff requires a valid HTTPS server URL")
	}
	tokenBytes := make([]byte, TokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "mint handoff token").LogError(ctx, m.logger)
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(ttl)
	createdBy := strings.TrimSpace(conv.PtrValOr(input.ActorEmail, ""))
	if createdBy == "" {
		createdBy = input.UserID
	}

	blobW, blobURL, err := m.blobStore.Write(ctx, "session-handoffs/"+token+".md", "text/markdown", int64(len(input.Content)))
	if err != nil {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "store handoff document").LogError(ctx, m.logger)
	}
	cleanupBlob := func() {
		if err := m.blobStore.Delete(context.WithoutCancel(ctx), blobURL); err != nil {
			m.logger.WarnContext(ctx, "orphaned handoff blob not deleted", attr.SlogError(err))
		}
	}
	if _, err := io.WriteString(blobW, input.Content); err != nil {
		_ = blobW.Close()
		cleanupBlob()
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "store handoff document").LogError(ctx, m.logger)
	}
	if err := blobW.Close(); err != nil {
		cleanupBlob()
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "store handoff document").LogError(ctx, m.logger)
	}

	dbtx, err := m.db.Begin(ctx)
	if err != nil {
		cleanupBlob()
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "store handoff").LogError(ctx, m.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	row, err := repo.New(dbtx).InsertSessionHandoffLink(ctx, repo.InsertSessionHandoffLinkParams{
		ProjectID: input.ProjectID, OrganizationID: input.OrganizationID, SessionID: input.SessionID,
		Token: token, BlobUrl: blobURL.String(), CreatedByEmail: createdBy, ExpiresAt: conv.ToPGTimestamptz(expiresAt),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		cleanupBlob()
		return SessionHandoffMintResult{}, oops.E(oops.CodeNotFound, err, "project not found")
	case err != nil:
		cleanupBlob()
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "store handoff").LogError(ctx, m.logger)
	}
	if err := m.audit.LogChatSessionHandoffExport(ctx, dbtx, audit.LogChatSessionHandoffExportEvent{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, input.UserID), ActorDisplayName: input.ActorEmail,
		ActorSlug: nil, ChatSessionURN: urn.NewChatSession(input.ChatID), ChatTitle: input.ChatTitle,
		SourceSurface: strings.TrimSpace(input.SourceSurface), ContentBytes: len(input.Content), TTLSeconds: int(ttl / time.Second),
		ExpiresAt: expiresAt, DeviceSerial: input.DeviceSerial, DeviceHostname: strings.TrimSpace(input.DeviceHostname),
	}); err != nil {
		cleanupBlob()
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "record handoff export").LogError(ctx, m.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "store handoff").LogError(ctx, m.logger)
	}
	return SessionHandoffMintResult{URL: publicHandoffURL(baseURL, token), ExpiresAt: row.ExpiresAt.Time.UTC().Format(time.RFC3339)}, nil
}
