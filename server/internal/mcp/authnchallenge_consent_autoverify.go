package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
)

// autoVerifications admits the probes committed grants start off the request
// path, remembers which client each one is for while it runs, and drains them
// once on shutdown. Admission and registration share one lock, so a probe is
// never admitted after the drain has started.
type autoVerifications struct {
	mu     sync.Mutex
	wg     sync.WaitGroup
	closed bool
}

func newAutoVerifications() *autoVerifications {
	return &autoVerifications{mu: sync.Mutex{}, wg: sync.WaitGroup{}, closed: false}
}

// admit runs fn on its own goroutine; false once admission is closed.
func (a *autoVerifications) admit(fn func()) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	a.wg.Go(func() {
		fn()
	})
	return true
}

// shutdown closes admission, then waits for every admitted probe or ctx.
func (a *autoVerifications) shutdown(ctx context.Context) error {
	a.mu.Lock()
	a.closed = true
	a.mu.Unlock()
	drained := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the caller names the drain
	}
}

// Shutdown stops admitting automatic verifications and the keepalive
// re-check, then drains the probes in flight. Run it after the HTTP servers
// have drained and before the database and cache close: the probes detach
// from their requests and still write verdicts and close upstream sessions.
func (s *Service) Shutdown(ctx context.Context) error {
	var errs []error
	if err := s.remoteSessionRecheck.shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("drain remote session re-checks: %w", err))
	}
	if err := s.autoVerifications.shutdown(ctx); err != nil {
		errs = append(errs, fmt.Errorf("drain automatic verifications: %w", err))
	}
	return errors.Join(errs...)
}

// verifyRemoteGrant probes a grant the remote login callback just committed, so
// a connect or reconnect carries its own verdict. Everything, placement
// included, runs off the request path under the probe's own budget; the caller
// is held for at most AutoVerifyWait so a fast member's verdict is on the first
// render. Best effort: anything that cannot be placed leaves the card at "Not
// yet verified" for the manual Verify.
func (s *Service) verifyRemoteGrant(ctx context.Context, grant remotesessions.RemoteGrant) time.Time {
	logger := s.logger.With(attr.SlogRemoteSessionClientID(grant.RemoteSessionClientID.String()))
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.metaRuntime.ValidationTimeout)
	deadline, _ := probeCtx.Deadline()
	done := make(chan struct{})
	admitted := s.autoVerifications.admit(func() {
		defer cancel()
		defer close(done)
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(probeCtx, "verify new remote grant panicked", attr.SlogError(fmt.Errorf("%v", recovered)))
			}
		}()
		endpoint, challengeState, ok := s.placeRemoteGrant(probeCtx, logger, grant)
		if ok {
			s.probeRemoteGrant(probeCtx, logger, endpoint, challengeState, grant)
		}
	})
	if !admitted {
		cancel()
		logger.InfoContext(ctx, "new remote grant not verified: shutting down")
		return time.Time{}
	}
	wait := time.NewTimer(s.metaRuntime.AutoVerifyWait)
	defer wait.Stop()
	select {
	case <-done:
		return time.Time{}
	case <-wait.C:
		logger.InfoContext(ctx, "new remote grant still verifying")
		return deadline
	}
}

// placeRemoteGrant finds the consent challenge the grant belongs to and the
// endpoint it was minted for, the same resolution the IdP callback trusts.
func (s *Service) placeRemoteGrant(ctx context.Context, logger *slog.Logger, grant remotesessions.RemoteGrant) (*ResolvedMcpEndpoint, AuthnChallengeState, bool) {
	var none AuthnChallengeState
	challengeState, err := s.authnChallengeCache.Get(ctx, "authnChallenge:"+grant.ParentChallengeID)
	if err != nil {
		logger.InfoContext(ctx, "new remote grant not verified: consent challenge not found", attr.SlogError(err))
		return nil, none, false
	}
	if challengeState.UserSessionIssuerID != grant.UserSessionIssuerID ||
		challengeState.Subject == nil || challengeState.Subject.String() != grant.Subject.String() {
		logger.InfoContext(ctx, "new remote grant not verified: grant does not belong to the consent challenge")
		return nil, none, false
	}
	endpoint, err := s.loadResolvedMcpEndpointByRef(ctx, challengeState.Endpoint)
	if err != nil {
		logger.InfoContext(ctx, "new remote grant not verified: endpoint not resolved", attr.SlogError(err))
		return nil, none, false
	}
	if err := endpoint.ValidateGlobalChallenge(ctx, s.db, challengeState.Endpoint, challengeState.UserSessionIssuerID); err != nil {
		logger.InfoContext(ctx, "new remote grant not verified: challenge no longer valid for its endpoint", attr.SlogError(err))
		return nil, none, false
	}
	return endpoint, challengeState, true
}

// probeRemoteGrant runs the probe for the grant's card on the endpoint.
func (s *Service) probeRemoteGrant(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, challengeState AuthnChallengeState, grant remotesessions.RemoteGrant) {
	clients, err := s.remoteChallengeMgr.ListClients(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
	if err != nil {
		logger.WarnContext(ctx, "new remote grant not verified: list clients", attr.SlogError(err))
		return
	}
	client := findConsentClient(clients, grant.RemoteSessionClientID)
	if client == nil {
		logger.InfoContext(ctx, "new remote grant not verified: client is not bound to this endpoint")
		return
	}
	// The probe logs its own refusals; this only says the verdict did not land.
	if err := s.probeRemoteSession(ctx, logger, endpoint, challengeState, *client, &grant, remotesessionmetrics.ValidationTriggerConnect); err != nil {
		logger.InfoContext(ctx, "new remote grant not verified", attr.SlogError(err))
	}
}
