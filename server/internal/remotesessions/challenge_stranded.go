package remotesessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	redisCache "github.com/go-redis/cache/v9"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// strandedLegWindow bounds how long an unanswered resource-bearing authorize
// leg can steer a restarted login. It matches RemoteLoginState's TTL: past it
// the leg's state has expired, so whether the issuer answered is unknowable.
const strandedLegWindow = 10 * time.Minute

// pendingResourceLeg remembers the last resource-bearing authorize leg one
// subject started for one client, issuer, and resource, so a restart can tell
// whether the issuer ever redirected back.
//
// Some issuers answer a resource they refuse with their own error page instead
// of an invalid_target redirect, so retryWithoutResource never runs and the
// user is stranded. A restart while that leg's state is still unconsumed is
// the only signal Gram gets.
//
// The fallback is once per window: FallbackTaken stays set until ExpiresAt,
// which is anchored to the first leg and never extended, so a window holds at
// most one resource-less leg and no resource/no-resource oscillation.
type pendingResourceLeg struct {
	// Key is the hashed subject, client, issuer, and resource binding.
	Key string `json:"key"`

	// StateID is the RemoteLoginState the leg was minted with. The callback
	// consumes that state, so its presence means the issuer never answered.
	StateID string `json:"state_id"`

	// FallbackTaken records that this window's resource-less leg was spent.
	FallbackTaken bool `json:"fallback_taken"`

	// ExpiresAt ends the window. It is set by the first leg and carried over
	// unchanged when the fallback is recorded.
	ExpiresAt time.Time `json:"expires_at"`
}

func (p pendingResourceLeg) CacheKey() string   { return "remoteLoginPendingResource:" + p.Key }
func (p pendingResourceLeg) TTL() time.Duration { return time.Until(p.ExpiresAt) }

// pendingResourceLegKey binds a leg to the subject, client, user session
// issuer, and resource. It is hashed so no subject identifier is stored in a
// cache key. The resource is part of the key so an issuer refusing one
// resource does not drop the resource from logins for another.
func pendingResourceLegKey(parent ParentChallenge, client Client) string {
	h := sha256.New()
	for _, part := range []string{parent.Subject.String(), client.ID.String(), parent.UserSessionIssuerID.String(), parent.Resource} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// sendsResource reports whether a fresh leg for (parent, client) puts the
// resource on the wire: the only shape whose rejection retryWithoutResource
// would recover, and so the only one the stranded fallback may stand in for.
func sendsResource(parent ParentChallenge, client Client) bool {
	operatorOmits := client.IssuerResourceIndicatorSupported != nil && !*client.IssuerResourceIndicatorSupported
	return parent.Resource != "" && !operatorOmits && parent.Subject != nil && !parent.Subject.IsZero()
}

// legPending reports whether the RemoteLoginState stateID is still
// unconsumed. A cache failure reads as answered, so an unhealthy cache never
// drops the resource.
func (m *ChallengeManager) legPending(ctx context.Context, logger *slog.Logger, stateID string) bool {
	_, err := m.cache.Get(ctx, "remoteLogin:"+stateID)
	switch {
	case err == nil:
		return true
	case errors.Is(err, redisCache.ErrCacheMiss):
		return false
	default:
		logger.WarnContext(ctx, "read prior remote login state", attr.SlogError(err))
		return false
	}
}

// strandedLegDecision is what BuildAuthorizationUrl learned from the prior leg.
type strandedLegDecision struct {
	// fallback mints this leg without the resource, as a ResourceRetried leg.
	fallback bool

	// record is the marker to store, with the minted leg's state, once the
	// leg is minted; nil stores none.
	record *pendingResourceLeg
}

// decideStrandedLeg inspects the subject's prior leg for this binding. It
// counts and logs a prior leg the issuer never answered, and chooses the
// resource-less fallback only when that leg sent the resource and this
// window's fallback is unspent.
func (m *ChallengeManager) decideStrandedLeg(ctx context.Context, parent ParentChallenge, client Client) strandedLegDecision {
	if !sendsResource(parent, client) {
		return strandedLegDecision{fallback: false, record: nil}
	}
	logger := m.logger.With(
		attr.SlogOAuthIssuer(client.IssuerURL),
		attr.SlogRemoteSessionIssuerID(client.RemoteSessionIssuerID.String()),
		attr.SlogRemoteSessionClientID(client.ID.String()),
		attr.SlogProjectID(parent.ProjectID.String()),
	)
	key := pendingResourceLegKey(parent, client)
	fresh := &pendingResourceLeg{Key: key, StateID: "", FallbackTaken: false, ExpiresAt: time.Now().Add(strandedLegWindow)}

	prior, err := m.pendingLegs.Get(ctx, pendingResourceLeg{Key: key, StateID: "", FallbackTaken: false, ExpiresAt: time.Time{}}.CacheKey())
	switch {
	case errors.Is(err, redisCache.ErrCacheMiss):
		return strandedLegDecision{fallback: false, record: fresh}
	case err != nil:
		// Recording nothing keeps a marker the read could not see, so a
		// transient failure cannot reset a spent fallback.
		logger.WarnContext(ctx, "read prior remote login leg", attr.SlogError(err))
		return strandedLegDecision{fallback: false, record: nil}
	}

	pending := m.legPending(ctx, logger, prior.StateID)
	if pending {
		m.metrics.RecordUnanswered(ctx, client.IssuerURL)
	}
	if prior.FallbackTaken {
		// The window's fallback is spent. The marker keeps its expiry and
		// flag and tracks only the newest leg, so the window cannot restart
		// early and each unanswered leg is counted once.
		if pending {
			logger.WarnContext(ctx, "identity provider never answered a login and this window's resource-less retry is spent; sending the resource")
		}
		return strandedLegDecision{fallback: false, record: &prior}
	}
	if !pending {
		return strandedLegDecision{fallback: false, record: fresh}
	}
	logger.WarnContext(ctx, "identity provider never answered a login that sent the RFC 8707 resource parameter; retrying the login without it",
		attr.SlogOAuthResource(parent.Resource),
	)
	prior.FallbackTaken = true
	return strandedLegDecision{fallback: true, record: &prior}
}
