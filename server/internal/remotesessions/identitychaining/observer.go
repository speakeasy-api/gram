package identitychaining

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// observeTimeout bounds an observer call so readiness bookkeeping never
// holds up a proxied request for long.
const observeTimeout = 2 * time.Second

// Observation is one fresh acquisition attempt. It carries no tokens, claims
// or provider response content.
type Observation struct {
	OrganizationID string

	// TrustedIssuerID is the identity provider the exchange was sent to.
	TrustedIssuerID uuid.UUID

	// RemoteIssuerID, RemoteIssuer and Resource are the upstream the grant
	// was for; RemoteIssuer is also the audience the exchange requested.
	RemoteIssuerID uuid.UUID
	RemoteIssuer   string
	Resource       string

	Outcome Outcome

	// GrantValidated reports that the identity provider issued an ID-JAG that
	// passed validation, so a redemption outcome is the resource
	// authorization server's answer.
	GrantValidated bool

	// StartedAt is when the attempt began, before any provider call.
	StartedAt time.Time
}

// Observer receives every fresh acquisition attempt; never credential-store
// hits, cached failures or a concurrent holder's result. The context still
// carries the proxied request's identity; an observer that audits must not
// act as it.
type Observer interface {
	ObserveAttempt(ctx context.Context, o Observation) error
}

// SetObserver registers an observer for fresh attempts.
func (c *Chainer) SetObserver(o Observer) {
	c.observer = o
}

func (c *Chainer) observe(ctx context.Context, logger *slog.Logger, o Observation) {
	if c.observer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), observeTimeout)
	defer cancel()
	if err := c.observer.ObserveAttempt(ctx, o); err != nil {
		logger.WarnContext(ctx, "observe identity chaining attempt", attr.SlogError(err))
	}
}
