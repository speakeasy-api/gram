package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	redisCache "github.com/go-redis/cache/v9"
	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const bindingRefreshLifetime = 10 * time.Minute

type bindingRefreshAttempt struct {
	ID              string             `json:"id"`
	OrganizationID  string             `json:"organization_id"`
	ProjectID       uuid.UUID          `json:"project_id"`
	PrincipalID     uuid.UUID          `json:"principal_id"`
	IssuerID        uuid.UUID          `json:"issuer_id"`
	BindingID       uuid.UUID          `json:"binding_id"`
	ClientID        uuid.UUID          `json:"client_id"`
	Subject         urn.SessionSubject `json:"subject"`
	CreatedAt       time.Time          `json:"created_at"`
	Started         bool               `json:"started"`
	RemoteSessionID uuid.UUID          `json:"remote_session_id"`
	GrantGeneration int64              `json:"grant_generation"`
}

func (a bindingRefreshAttempt) CacheKey() string { return "bindingRefresh:" + a.ID }
func (a bindingRefreshAttempt) TTL() time.Duration {
	return time.Until(a.CreatedAt.Add(bindingRefreshLifetime))
}

// BindingRefreshAttempts binds a dashboard's one-time attachment decision to
// the OAuth parent challenge and the exact grant its callback committed.
type BindingRefreshAttempts struct {
	attempts cache.TypedCacheObject[bindingRefreshAttempt]
}

func NewBindingRefreshAttempts(logger *slog.Logger, adapter cache.Cache) *BindingRefreshAttempts {
	return &BindingRefreshAttempts{attempts: cache.NewTypedObjectCache[bindingRefreshAttempt](logger, adapter, cache.SuffixNone)}
}

// Start claims the attempt for an endpoint. Its random ID is retained in the
// server-owned OAuth challenge, independently of rotating challenge IDs.
func (s *BindingRefreshAttempts) Start(ctx context.Context, id, organizationID string, projectID, issuerID uuid.UUID) error {
	attempt, err := s.attempts.Get(ctx, "bindingRefresh:"+id)
	if err != nil && !errors.Is(err, redisCache.ErrCacheMiss) {
		return fmt.Errorf("read connection refresh attempt: %w", err)
	}
	if err != nil || attempt.TTL() <= 0 || attempt.Started || attempt.OrganizationID != organizationID || attempt.ProjectID != projectID || attempt.IssuerID != issuerID {
		return oops.E(oops.CodeForbidden, nil, "connection refresh attempt is unavailable")
	}
	started := attempt
	started.Started = true
	swapped, err := s.attempts.CompareAndSwap(ctx, attempt, started)
	if err != nil {
		return fmt.Errorf("start connection refresh attempt: %w", err)
	}
	if !swapped {
		return oops.E(oops.CodeConflict, nil, "connection refresh attempt already started")
	}
	return nil
}

// Record accepts only the expected person's client grant. The attempt ID must
// come from the grant's stored parent challenge, never a browser parameter.
// Compare-and-swap prevents a late callback from resurrecting a consumed attempt.
func (s *BindingRefreshAttempts) Record(ctx context.Context, attemptID string, grant RemoteGrant) error {
	attempt, err := s.attempts.Get(ctx, "bindingRefresh:"+attemptID)
	if err != nil {
		if errors.Is(err, redisCache.ErrCacheMiss) {
			return nil
		}
		return fmt.Errorf("read connection refresh attempt: %w", err)
	}
	if attempt.TTL() <= 0 || !attempt.Started || attempt.GrantGeneration != 0 || attempt.Subject != grant.Subject || attempt.IssuerID != grant.UserSessionIssuerID || attempt.ClientID != grant.RemoteSessionClientID {
		return nil
	}
	completed := attempt
	completed.RemoteSessionID = grant.RemoteSessionID
	completed.GrantGeneration = grant.GrantGeneration
	if _, err := s.attempts.CompareAndSwap(ctx, attempt, completed); err != nil {
		return fmt.Errorf("record connection refresh grant: %w", err)
	}
	return nil
}

// PendingClient identifies the one client whose fresh OAuth callback this
// first-party challenge still needs, even when another sign-in made its row live.
func (s *BindingRefreshAttempts) PendingClient(ctx context.Context, challengeID string, subject urn.SessionSubject) (uuid.UUID, error) {
	attempt, err := s.attempts.Get(ctx, "bindingRefresh:"+challengeID)
	if err != nil {
		if errors.Is(err, redisCache.ErrCacheMiss) {
			return uuid.Nil, nil
		}
		return uuid.Nil, fmt.Errorf("read pending connection refresh: %w", err)
	}
	if attempt.TTL() <= 0 || !attempt.Started || attempt.GrantGeneration != 0 {
		return uuid.Nil, nil
	}
	if attempt.Subject != subject {
		return uuid.Nil, oops.E(oops.CodeForbidden, nil, "sign in as the person who started this connection refresh")
	}
	return attempt.ClientID, nil
}
