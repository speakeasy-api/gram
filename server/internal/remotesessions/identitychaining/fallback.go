package identitychaining

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
)

const (
	// maxProviderAttempts bounds tries against one provider: a transient
	// failure is retried once before the next provider in the order.
	maxProviderAttempts = 2

	// retryBackoff is the mean pause before retrying a transient failure;
	// the actual pause is jittered between half and one and a half times it.
	retryBackoff = 200 * time.Millisecond

	// minRetryBudget is the least time a retry needs to be worth starting.
	// With less left in the provider's share of the deadline, the next
	// provider is tried instead.
	minRetryBudget = 2 * time.Second
)

// providerAttempt runs one try against one provider in the order.
type providerAttempt func(ctx context.Context, logger *slog.Logger, p remotesessions.EMAProvider) (Token, Outcome)

// ProviderFailure is one provider's failure when every provider in the order
// failed.
type ProviderFailure struct {
	Provider    remotesessions.EMAProviderKind              `json:"provider"`
	Reason      Reason                                      `json:"reason"`
	Unavailable remotesessions.EMAProviderUnavailableReason `json:"unavailable,omitempty"`
}

// tryProviders tries the order's providers in turn within ctx's deadline
// until one succeeds. A provider that cannot serve, including one the
// endpoint's authorization server does not trust, is skipped. Each provider
// gets an equal share of the time left, so a slow provider never starves the
// rest, and its transient failures from before any token request are retried
// within that share. Any other failure moves to the next provider at once.
//
// Identity ineligibility needs no special case: it is the Speakeasy issuer's
// judgment of the human's sign-in, and a de-duplicated order names that
// issuer at most once. External providers vouch from their own sign-in, so
// they are still tried after it.
func (c *Chainer) tryProviders(ctx context.Context, logger *slog.Logger, path string, order remotesessions.EMAProviderOrder, try providerAttempt) (Token, Outcome) {
	var none Token
	failures := make([]Outcome, 0, len(order.Providers))
	for i, p := range order.Providers {
		fallback := i > 0
		providerLogger := logger.With(attr.SlogIdentityChainingProvider(string(p.Kind)))
		if !p.Available() {
			outcome := unavailableBecause(p.Unavailable)
			outcome.Provider = p.Kind
			c.recordAttempt(ctx, path, outcome, fallback, 0)
			failures = append(failures, outcome)
			continue
		}
		providerCtx, cancel := context.WithTimeout(ctx, providerBudget(ctx, availableFrom(order.Providers[i:])))
		token, outcome, retries := c.tryProvider(providerCtx, providerLogger, p, try)
		cancel()
		outcome.Provider = p.Kind
		c.recordAttempt(ctx, path, outcome, fallback, retries)
		if outcome.Succeeded() {
			if fallback {
				providerLogger.InfoContext(ctx, "identity chaining served by a fallback ema provider",
					attr.SlogIdentityChainingFallback(true),
					attr.SlogIdentityChainingRetries(retries),
					attr.SlogIdentityChainingFailures(failureSummary(failures)),
				)
			}
			return token, outcome
		}
		failures = append(failures, outcome)
		if ctx.Err() != nil {
			break
		}
	}
	outcome := exhausted(failures)
	if len(failures) > 1 {
		logger.WarnContext(ctx, "identity chaining failed with every ema provider",
			attr.SlogOutcome(string(outcome.Reason)),
			attr.SlogIdentityChainingFailures(failureSummary(failures)),
		)
	}
	return none, outcome
}

// tryProvider runs try against p, retrying a transient failure while
// attempts and p's share of the deadline remain. It reports the retries made.
func (c *Chainer) tryProvider(ctx context.Context, logger *slog.Logger, p remotesessions.EMAProvider, try providerAttempt) (Token, Outcome, int) {
	retries := 0
	for {
		token, outcome := try(ctx, logger, p)
		if !retryable(outcome) || retries+1 >= maxProviderAttempts {
			return token, outcome, retries
		}
		pause := jitter(retryBackoff)
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < pause+minRetryBudget {
			return token, outcome, retries
		}
		retries++
		logger.InfoContext(ctx, "retrying identity chaining after a transient failure",
			attr.SlogOutcome(string(outcome.Reason)),
			attr.SlogReason(string(outcome.Stage)),
			attr.SlogIdentityChainingRetries(retries),
		)
		timer := time.NewTimer(pause)
		select {
		case <-ctx.Done():
			timer.Stop()
			return token, outcome, retries
		case <-timer.C:
		}
	}
}

// retryable reports a transient failure that is safe to repeat: one that
// happened before any token request could have reached the provider. A
// failure after submission is ambiguous, and repeating it could mint a
// duplicate grant against the provider's quota, so it, a shared failure
// answered from the cache, stale configuration and every provider rejection
// move to the next provider instead.
func retryable(o Outcome) bool {
	return o.Reason == ReasonTransientFailure && o.Retryable && !o.Cached && !o.Submitted
}

// jitter spreads retries between half and one and a half times d.
func jitter(d time.Duration) time.Duration {
	return d/2 + time.Duration(rand.Int64N(int64(d))) // #nosec G404 -- retry jitter is not security-sensitive
}

// availableFrom counts the providers that can serve.
func availableFrom(providers []remotesessions.EMAProvider) int {
	n := 0
	for _, p := range providers {
		if p.Available() {
			n++
		}
	}
	return n
}

// providerBudget is one provider's share of the time ctx has left, split
// evenly across the providers still to try.
func providerBudget(ctx context.Context, providersLeft int) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return acquireTimeout
	}
	left := time.Until(deadline)
	if providersLeft > 1 {
		left /= time.Duration(providersLeft)
	}
	return left
}

// exhausted reports an order where no provider succeeded. A provider asking
// for reauthentication wins, since reconnecting is the one step the user can
// take. Otherwise the first provider that was actually tried explains the
// failure best; when none could serve, the result is provider_unavailable,
// so the interactive path applies unchanged. Every provider's failure is
// attached.
func exhausted(failures []Outcome) Outcome {
	if len(failures) == 0 {
		return providerUnavailable
	}
	outcome := failures[0]
	for _, f := range failures {
		if f.Reason != ReasonProviderUnavailable {
			outcome = f
			break
		}
	}
	if i := slices.IndexFunc(failures, func(f Outcome) bool { return f.Reason == ReasonReauthenticationRequired }); i >= 0 {
		outcome = failures[i]
	}
	if len(failures) > 1 {
		outcome.Failures = make([]ProviderFailure, 0, len(failures))
		for _, f := range failures {
			outcome.Failures = append(outcome.Failures, ProviderFailure{Provider: f.Provider, Reason: f.Reason, Unavailable: f.Unavailable})
		}
	}
	return outcome
}

// failureSummary renders failures as provider:reason pairs for logs.
func failureSummary(failures []Outcome) string {
	parts := make([]string, 0, len(failures))
	for _, f := range failures {
		part := string(f.Provider) + ":" + string(f.Reason)
		if f.Unavailable != "" {
			part += "(" + string(f.Unavailable) + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ",")
}

// recordAttempt counts one provider's attempt by outcome, whether an earlier
// provider in the order failed, retries and, when it could not serve, why.
func (c *Chainer) recordAttempt(ctx context.Context, path string, outcome Outcome, fallback bool, retries int) {
	attrs := []attribute.KeyValue{
		attr.IdentityChainingPath(path), attr.IdentityChainingProvider(string(outcome.Provider)), attr.Outcome(outcome.Reason),
		attr.IdentityChainingFallback(fallback), attr.IdentityChainingRetries(retries),
	}
	if outcome.Unavailable != "" {
		attrs = append(attrs, attr.IdentityChainingUnavailableReason(string(outcome.Unavailable)))
	}
	c.attempts.Add(ctx, 1, metric.WithAttributes(attrs...))
}

// reuseStored releases a still-valid stored credential issued by a provider
// after the order's first that can serve. A credential is bound to the
// provider that issued it, and reused only while that provider is in the
// order and can serve and its provenance still holds. The first provider's
// own credential is checked by its attempt, so an order whose first provider
// keeps failing still serves the credential a fallback provider issued,
// without retrying the failing provider on every request.
func (c *Chainer) reuseStored(ctx context.Context, logger *slog.Logger, req Request, sel selection, order remotesessions.EMAProviderOrder) (Token, Outcome, bool) {
	var none Token
	first := true
	for i, p := range order.Providers {
		if !p.Available() {
			continue
		}
		if first {
			first = false
			continue
		}
		providerLogger := logger.With(attr.SlogIdentityChainingProvider(string(p.Kind)))
		src := c.source(p)
		current := sel
		current.provider = p
		if !src.authorize(ctx, providerLogger, req, &current).Succeeded() {
			continue
		}
		token, ok := c.cachedToken(ctx, providerLogger, req, current)
		if !ok {
			continue
		}
		token, outcome := c.release(ctx, providerLogger, req, current, src, token)
		if !outcome.Succeeded() {
			continue
		}
		outcome.Provider = p.Kind
		c.recordAttempt(ctx, "acquire", outcome, i > 0, 0)
		return token, outcome, true
	}
	return none, success, false
}

// release hands out a stored or concurrently acquired token only while its
// provider's authority still holds and the provider is still in the order
// and can serve, so a revocation or an order changed meanwhile never hands
// one out.
func (c *Chainer) release(ctx context.Context, logger *slog.Logger, req Request, sel selection, src tokenSource, token Token) (Token, Outcome) {
	var none Token
	if outcome := src.checkReuse(ctx, req, sel); !outcome.Succeeded() {
		return none, outcome
	}
	if outcome := c.stillServes(ctx, logger, req, sel); !outcome.Succeeded() {
		return none, outcome
	}
	return token, success
}
