package sessionhandoff

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
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
	OrganizationID       string
	ProjectID            uuid.UUID
	UserID               string
	ActorEmail           *string
	ChatID               uuid.UUID
	SessionID            string
	ChatTitle            string
	Content              string
	TTLSeconds           *int
	SourceSurface        string
	DeviceSerial         string
	DeviceHostname       string
	IdempotencyOperation string
	IdempotencyKey       string
	IdempotencyInputHash string
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

	if input.UserID == "" {
		return SessionHandoffMintResult{}, oops.E(oops.CodeBadRequest, nil, "session handoff creator user id is required")
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.IdempotencyKey == "" {
		input.IdempotencyOperation = ""
		if input.IdempotencyInputHash != "" {
			return SessionHandoffMintResult{}, oops.E(oops.CodeBadRequest, nil, "idempotency input hash requires an idempotency key")
		}
	} else if (input.IdempotencyOperation != "platform_create" && input.IdempotencyOperation != "device_create") || len(input.IdempotencyKey) > MaxIdempotencyKeyBytes || !validInputHash(input.IdempotencyInputHash) {
		return SessionHandoffMintResult{}, oops.E(oops.CodeBadRequest, nil, "invalid idempotency key or input hash")
	}

	if input.IdempotencyKey != "" {
		existing, err := m.lookupIdempotentMint(ctx, input, baseURL)
		if err == nil {
			return existing, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return SessionHandoffMintResult{}, err
		}
	}

	tokenBytes := make([]byte, TokenBytes)
	if _, err := rand.Read(tokenBytes); err != nil {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "mint handoff token").LogError(ctx, m.logger)
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(ttl)
	createdBy := strings.TrimSpace(conv.PtrValOr(input.ActorEmail, ""))

	objectPath := "session-handoffs/" + token + ".md"
	expectedBlobURL, err := m.blobStore.Location(objectPath)
	if err != nil {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "resolve handoff upload location").LogError(ctx, m.logger)
	}
	intentID, err := repo.New(m.db).CreateSessionHandoffUploadIntent(ctx, repo.CreateSessionHandoffUploadIntentParams{
		ActorUserID: input.UserID, ObjectPath: objectPath, BlobUrl: expectedBlobURL.String(),
		ProjectID: input.ProjectID, OrganizationID: input.OrganizationID,
	})
	if err != nil {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "record handoff upload intent").LogError(ctx, m.logger)
	}
	blobW, blobURL, err := m.blobStore.Write(ctx, objectPath, "text/markdown", int64(len(input.Content)))
	if err != nil {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "store handoff document").LogError(ctx, m.logger)
	}
	if blobURL.String() != expectedBlobURL.String() {
		closeErr := blobW.Close()
		deleteErr := m.blobStore.Delete(context.WithoutCancel(ctx), blobURL)
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, errors.Join(closeErr, deleteErr), "blob store returned a location different from the durable upload intent").LogError(ctx, m.logger)
	}
	if _, err := io.WriteString(blobW, input.Content); err != nil {
		_ = blobW.Close()
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "store handoff document").LogError(ctx, m.logger)
	}
	if err := blobW.Close(); err != nil {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "store handoff document").LogError(ctx, m.logger)
	}

	dbtx, err := m.db.Begin(ctx)
	if err != nil {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "store handoff").LogError(ctx, m.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	row, err := repo.New(dbtx).InsertSessionHandoffLink(ctx, repo.InsertSessionHandoffLinkParams{
		ProjectID: input.ProjectID, OrganizationID: input.OrganizationID, SessionID: input.SessionID,
		Token: token, BlobUrl: blobURL.String(), ActorUserID: conv.ToPGText(input.UserID), CreatedByEmail: createdBy,
		IdempotencyOperation: conv.ToPGTextEmpty(input.IdempotencyOperation), IdempotencyKey: conv.ToPGTextEmpty(input.IdempotencyKey), IdempotencyInputHash: conv.ToPGTextEmpty(input.IdempotencyInputHash),
		ExpiresAt: conv.ToPGTimestamptz(expiresAt), UploadIntentID: intentID,
	})
	if errors.Is(err, pgx.ErrNoRows) && input.IdempotencyKey != "" {
		existing, lookupErr := lookupIdempotentMint(ctx, repo.New(dbtx), input, baseURL)
		if lookupErr == nil {
			return existing, nil
		}
		if !errors.Is(lookupErr, pgx.ErrNoRows) {
			return SessionHandoffMintResult{}, lookupErr
		}
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return SessionHandoffMintResult{}, oops.E(oops.CodeNotFound, err, "project not found")
	case err != nil:
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "store handoff").LogError(ctx, m.logger)
	}
	if err := m.audit.LogChatSessionHandoffExport(ctx, dbtx, audit.LogChatSessionHandoffExportEvent{
		OrganizationID: input.OrganizationID, ProjectID: input.ProjectID,
		Actor: urn.NewPrincipal(urn.PrincipalTypeUser, input.UserID), ActorDisplayName: input.ActorEmail,
		ActorSlug: nil, ChatSessionURN: urn.NewChatSession(input.ChatID), ChatTitle: input.ChatTitle,
		SourceSurface: strings.TrimSpace(input.SourceSurface), ContentBytes: len(input.Content), TTLSeconds: int(ttl / time.Second),
		ExpiresAt: expiresAt, DeviceSerial: input.DeviceSerial, DeviceHostname: strings.TrimSpace(input.DeviceHostname),
	}); err != nil {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "record handoff export").LogError(ctx, m.logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		// The server cannot know whether COMMIT reached Postgres. A keyed request
		// resolves that ambiguity with a fresh connection and never deletes the
		// candidate blob, which may now be referenced by a committed row.
		if input.IdempotencyKey != "" {
			reconcileCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			existing, lookupErr := m.lookupIdempotentMint(reconcileCtx, input, baseURL)
			if lookupErr == nil {
				return existing, nil
			}
		}
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, err, "session handoff commit outcome is uncertain; retry with the same idempotency key").LogError(ctx, m.logger)
	}
	return SessionHandoffMintResult{URL: publicHandoffURL(baseURL, row.Token), ExpiresAt: row.ExpiresAt.Time.UTC().Format(time.RFC3339)}, nil
}

func (m *SessionHandoffMinter) lookupIdempotentMint(ctx context.Context, input SessionHandoffMintInput, baseURL *url.URL) (SessionHandoffMintResult, error) {
	return lookupIdempotentMint(ctx, repo.New(m.db), input, baseURL)
}

func lookupIdempotentMint(ctx context.Context, queries *repo.Queries, input SessionHandoffMintInput, baseURL *url.URL) (SessionHandoffMintResult, error) {
	row, err := queries.GetSessionHandoffLinkByIdempotencyKey(ctx, repo.GetSessionHandoffLinkByIdempotencyKeyParams{
		OrganizationID:       input.OrganizationID,
		ProjectID:            input.ProjectID,
		ActorUserID:          conv.ToPGText(input.UserID),
		IdempotencyOperation: conv.ToPGText(input.IdempotencyOperation),
		IdempotencyKey:       conv.ToPGText(input.IdempotencyKey),
	})
	if err != nil {
		return SessionHandoffMintResult{}, fmt.Errorf("look up session handoff idempotency key: %w", err)
	}
	if conv.FromPGTextOrEmpty[string](row.IdempotencyInputHash) != input.IdempotencyInputHash {
		return SessionHandoffMintResult{}, oops.E(oops.CodeConflict, nil, "idempotency key was already used for a different session handoff request")
	}
	return SessionHandoffMintResult{
		URL:       publicHandoffURL(baseURL, row.Token),
		ExpiresAt: row.ExpiresAt.Time.UTC().Format(time.RFC3339),
	}, nil
}

// Replay returns a previously committed keyed result without requiring the
// original project to still exist. Platform idempotency is organization-wide.
func (m *SessionHandoffMinter) Replay(ctx context.Context, organizationID, userID, operation, key, inputHash string) (SessionHandoffMintResult, error) {
	baseURL, ok := validHTTPSBaseURL(m.serverURL)
	if !ok {
		return SessionHandoffMintResult{}, oops.E(oops.CodeUnexpected, nil, "session handoff requires a valid HTTPS server URL")
	}
	return lookupIdempotentMint(ctx, repo.New(m.db), SessionHandoffMintInput{
		OrganizationID:       organizationID,
		ProjectID:            uuid.Nil,
		UserID:               userID,
		ActorEmail:           nil,
		ChatID:               uuid.Nil,
		SessionID:            "",
		ChatTitle:            "",
		Content:              "",
		TTLSeconds:           nil,
		IdempotencyOperation: operation,
		IdempotencyKey:       key,
		IdempotencyInputHash: inputHash,
		SourceSurface:        "",
		DeviceSerial:         "",
		DeviceHostname:       "",
	}, baseURL)
}
