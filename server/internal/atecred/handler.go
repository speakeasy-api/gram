package atecred

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// maxMintRequestBytes bounds the JSON request body accepted by
	// POST /internal/assistants/ate/credentials.
	maxMintRequestBytes = 4 << 10 // 4 KiB
)

var (
	// ErrActorCredentialDenied marks a permanent actor, thread, assistant, user,
	// or organization refusal when minting an ATE actor turn credential (HTTP 403).
	ErrActorCredentialDenied = errors.New("ATE actor credential mint denied")

	// ErrActorCredentialUnavailable marks a transient dependency failure during
	// ATE actor turn credential minting (HTTP 503).
	ErrActorCredentialUnavailable = errors.New("ATE actor credential mint unavailable")
)

// ActorCredentialMinter mints a short-lived Speakeasy turn credential for one
// live ATE thread actor.
type ActorCredentialMinter interface {
	// MintATEActorCredential validates actor liveness and tenancy, then mints a
	// short-lived principal credential bound to the actor's thread.
	MintATEActorCredential(ctx context.Context, atespace, actorName, actorUID string) (string, time.Time, error)
}

// NewMintHandler returns the HTTP handler for POST /internal/assistants/ate/credentials.
// It enforces Option A Google OIDC ID token authentication (or local-only static
// token in local development), validates the request shape, delegates minting to
// minter, and never logs the minted credential.
func NewMintHandler(logger *slog.Logger, auth *Authenticator, minter ActorCredentialMinter) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if auth == nil || minter == nil {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := auth.Authenticate(ctx, r.Header.Get("Authorization")); err != nil {
			logger.WarnContext(ctx, "ATE credential mint auth rejected", attr.SlogError(err))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		defer o11y.NoLogDefer(func() error { return r.Body.Close() })
		dec := json.NewDecoder(io.LimitReader(r.Body, maxMintRequestBytes))
		dec.DisallowUnknownFields()
		var req MintRequest
		if err := dec.Decode(&req); err != nil {
			logger.WarnContext(ctx, "ATE credential mint invalid request body", attr.SlogError(err))
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if !ValidATEName(req.Atespace) || !ValidATEName(req.ActorName) || !ValidActorUID(req.ActorUID) {
			http.Error(w, "invalid actor reference", http.StatusBadRequest)
			return
		}

		cred, expiresAt, err := minter.MintATEActorCredential(ctx, req.Atespace, req.ActorName, req.ActorUID)
		if err != nil {
			switch {
			case errors.Is(err, ErrActorCredentialDenied):
				logger.WarnContext(ctx, "ATE credential mint denied", attr.SlogError(err))
				http.Error(w, "forbidden", http.StatusForbidden)
			case errors.Is(err, ErrActorCredentialUnavailable):
				logger.ErrorContext(ctx, "ATE credential mint unavailable", attr.SlogError(err))
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			default:
				logger.ErrorContext(ctx, "ATE credential mint failed", attr.SlogError(err))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(MintResponse{
			Credential: cred,
			ExpiresAt:  expiresAt.UTC(),
		})
	})
}
