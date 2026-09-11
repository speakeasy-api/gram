// Keepalive re-check: idle grants with no refresh token are presented to their upstream on a slow cadence (AIM-261).

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/networkingress"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

const (
	// remoteSessionRecheckTick is how often one replica looks for due grants; the jitter keeps a fleet from claiming together.
	remoteSessionRecheckTick       = 5 * time.Minute
	remoteSessionRecheckTickJitter = 2 * time.Minute

	// remoteSessionRecheckBatch bounds one claim; a pass claims again while batches come back full and the budget holds.
	remoteSessionRecheckBatch = int32(20)

	// remoteSessionRecheckSlots bounds the probes one replica runs at once.
	remoteSessionRecheckSlots = 2

	// remoteSessionRecheckPassBudget stops a pass claiming more before the next tick would.
	remoteSessionRecheckPassBudget = 3 * time.Minute

	// remoteSessionRecheckPlacementBudget is what one re-check may spend before the probe's own timeout, on the re-read and endpoint resolution.
	remoteSessionRecheckPlacementBudget = 10 * time.Second
)

// remoteSessionRecheckHostRate caps probes per issuer host per replica, so one provider is never swept in a burst.
var remoteSessionRecheckHostRate = ratelimit.PerMinute(10)

// remoteSessionRecheck runs the sweep on the server process: the probe needs endpoint
// routing and the proxy builders, and no worker-to-server credential exists to call it remotely.
type remoteSessionRecheck struct {
	interval time.Duration
	// limiter paces probes per issuer host; nil without Redis.
	limiter *ratelimit.Limiter
	slots   chan struct{}
	// mu guards closed and every wg.Add, so a probe is never admitted after the drain has started.
	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
	stop   chan struct{}
}

func newRemoteSessionRecheck(interval time.Duration, redisClient *redis.Client, meterProvider metric.MeterProvider) *remoteSessionRecheck {
	var limiter *ratelimit.Limiter
	if redisClient != nil {
		limiter = ratelimit.New(ratelimit.NewRedisStore(redisClient), "remote_session_recheck_host", remoteSessionRecheckHostRate, ratelimit.WithMetrics(meterProvider))
	}
	return &remoteSessionRecheck{
		interval: interval,
		limiter:  limiter,
		slots:    make(chan struct{}, remoteSessionRecheckSlots),
		mu:       sync.Mutex{},
		closed:   false,
		wg:       sync.WaitGroup{},
		stop:     make(chan struct{}),
	}
}

// sleepUnlessStopped waits d; false when ctx ended first.
func sleepUnlessStopped(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// admit registers fn under the drain; false once shutdown has started.
func (r *remoteSessionRecheck) admit(fn func()) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false
	}
	r.wg.Go(fn)
	return true
}

// shutdown stops the loop and waits for the pass and probes in flight, or ctx.
func (r *remoteSessionRecheck) shutdown(ctx context.Context) error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.stop)
	}
	r.mu.Unlock()
	drained := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(drained)
	}()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the caller names the drain
	}
}

// StartRemoteSessionRecheck runs the keepalive re-check loop until Shutdown or
// ctx ends. The first pass waits a random fraction of a tick so replicas that
// boot together do not sweep together.
func (s *Service) StartRemoteSessionRecheck(ctx context.Context) {
	r := s.remoteSessionRecheck
	logger := s.logger.With(attr.SlogComponent("remote_session_recheck"))
	admitted := r.admit(func() {
		loopCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			select {
			case <-r.stop:
				cancel()
			case <-loopCtx.Done():
			}
		}()
		wait := randomDuration(remoteSessionRecheckTick)
		for sleepUnlessStopped(loopCtx, wait) {
			checked, err := s.SweepRemoteSessionRechecks(loopCtx)
			if err != nil {
				logger.ErrorContext(loopCtx, "remote session re-check pass failed", attr.SlogError(err))
			}
			if checked > 0 {
				logger.InfoContext(loopCtx, "remote session re-check pass finished", attr.SlogRemoteSessionRecheckCount(checked))
			}
			wait = remoteSessionRecheckTick - remoteSessionRecheckTickJitter + randomDuration(2*remoteSessionRecheckTickJitter)
		}
	})
	if !admitted {
		logger.InfoContext(ctx, "remote session re-check not started: shutting down")
	}
}

func randomDuration(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(limit))) // #nosec G404 -- sweep jitter is not security-sensitive
}

// SweepRemoteSessionRechecks runs one pass: claim due grants in batches and
// probe each under the slot cap, until a batch comes back short, the pass
// budget runs out, or the loop is stopped. Returns how many probes ran.
func (s *Service) SweepRemoteSessionRechecks(ctx context.Context) (int, error) {
	r := s.remoteSessionRecheck
	logger := s.logger.With(attr.SlogComponent("remote_session_recheck"))
	startedAt := time.Now()
	checked := 0
	for {
		now := time.Now()
		rows, err := remotesessions_repo.New(s.db).ClaimDueRemoteSessionRecheckCandidates(ctx, remotesessions_repo.ClaimDueRemoteSessionRecheckCandidatesParams{
			NowTs:         conv.ToPGTimestamptz(now),
			RecheckCutoff: conv.ToPGTimestamptz(now.Add(-r.interval)),
			// A claimed row that was skipped or left inconclusive comes back well before it is due again.
			AttemptCutoff: conv.ToPGTimestamptz(now.Add(-r.interval / 4)),
			LimitValue:    remoteSessionRecheckBatch,
		})
		if err != nil {
			return checked, fmt.Errorf("claim due remote session re-check candidates: %w", err)
		}
		var wg sync.WaitGroup
		for _, row := range rows {
			select {
			case r.slots <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				return checked, nil
			}
			checked++
			wg.Go(func() {
				defer func() { <-r.slots }()
				s.recheckRemoteSession(ctx, logger, row)
			})
		}
		wg.Wait()
		if len(rows) < int(remoteSessionRecheckBatch) || time.Since(startedAt) >= remoteSessionRecheckPassBudget || ctx.Err() != nil {
			return checked, nil
		}
	}
}

// recheckRemoteSession re-reads one claimed grant, places it on an endpoint it
// still serves, and runs the probe. Every refusal is logged and leaves the
// row for the next attempt window; nothing here writes a rejection.
func (s *Service) recheckRemoteSession(ctx context.Context, logger *slog.Logger, row remotesessions_repo.ClaimDueRemoteSessionRecheckCandidatesRow) {
	logger = logger.With(attr.SlogRemoteSessionID(row.ID.String()), attr.SlogOrganizationID(row.OrganizationID), attr.SlogOAuthIssuer(row.IssuerUrl))
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), remoteSessionRecheckPlacementBudget+s.metaRuntime.ValidationTimeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.ErrorContext(ctx, "remote session re-check panicked", attr.SlogError(fmt.Errorf("%v", recovered)))
		}
	}()

	if s.remoteSessionRecheck.limiter != nil {
		res, err := s.remoteSessionRecheck.limiter.Allow(ctx, issuerHost(row.IssuerUrl))
		switch {
		case err != nil:
			logger.WarnContext(ctx, "remote session re-check limiter unavailable; allowing", attr.SlogError(err))
		case !res.Allowed:
			logger.InfoContext(ctx, "remote session re-check skipped: issuer host rate limited")
			return
		}
	}

	now := time.Now()
	candidate, err := remotesessions_repo.New(s.db).GetDueRemoteSessionRecheckCandidate(ctx, remotesessions_repo.GetDueRemoteSessionRecheckCandidateParams{
		ID:             row.ID,
		NowTs:          conv.ToPGTimestamptz(now),
		RecheckCutoff:  conv.ToPGTimestamptz(now.Add(-s.remoteSessionRecheck.interval)),
		OrganizationID: row.OrganizationID,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.InfoContext(ctx, "remote session re-check skipped: no longer due")
		return
	case err != nil:
		logger.ErrorContext(ctx, "reload remote session for re-check", attr.SlogError(err))
		return
	}
	sess := candidate.RemoteSession
	logger = logger.With(attr.SlogRemoteSessionClientID(sess.RemoteSessionClientID.String()))

	placement, err := remotesessions_repo.New(s.db).GetRemoteSessionRecheckEndpoint(ctx, remotesessions_repo.GetRemoteSessionRecheckEndpointParams{
		UserSessionIssuerID:   sess.UserSessionIssuerID,
		RemoteSessionClientID: sess.RemoteSessionClientID,
		OrganizationID:        row.OrganizationID,
		SubjectUrn:            sess.SubjectUrn,
		NowTs:                 conv.ToPGTimestamptz(now),
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		logger.InfoContext(ctx, "remote session re-check skipped: no endpoint serves this grant")
		return
	case err != nil:
		logger.ErrorContext(ctx, "find endpoint for remote session re-check", attr.SlogError(err))
		return
	}
	// The runtime resolver re-applies visibility, network access and the issuer gate from the ref, as the callback does.
	// A zero authority is the public surface; a private-only endpoint refuses the ref and the grant is skipped.
	var publicAuthority networkingress.Authority
	ref := EndpointRef{
		CustomDomainID:  placement.CustomDomainID,
		Authority:       publicAuthority,
		BaseURL:         "",
		McpServerID:     placement.McpServerID,
		MetaMcpServerID: placement.MetaMcpServerID,
		IsPublic:        nil,
		ToolsetID:       uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		McpSlug:         placement.Slug,
		RouteBase:       "mcp",
	}
	endpoint, err := s.loadResolvedMcpEndpointByRef(ctx, ref)
	if err != nil {
		logger.InfoContext(ctx, "remote session re-check skipped: endpoint not resolved", attr.SlogError(err))
		return
	}
	clients, err := s.remoteChallengeMgr.ListClients(ctx, endpoint.ProjectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID)
	if err != nil {
		logger.ErrorContext(ctx, "list clients for remote session re-check", attr.SlogError(err))
		return
	}
	client := findConsentClient(clients, sess.RemoteSessionClientID)
	if client == nil {
		logger.InfoContext(ctx, "remote session re-check skipped: client is not bound to the endpoint")
		return
	}
	subject := sess.SubjectUrn
	// A synthetic first-party state: the probe reads only the subject, the id it keys its session on, and the endpoint.
	state := AuthnChallengeState{
		ID:                       "keepalive:" + sess.ID.String(),
		FlowID:                   "",
		UserSessionIssuerID:      endpoint.UserSessionIssuerID,
		AuthorizerUserID:         "",
		AuthorizerImpersonated:   nil,
		AgentAuthorizationTarget: nil,
		Endpoint:                 ref,
		ClientID:                 "",
		RedirectURI:              "",
		State:                    "",
		CodeChallenge:            "",
		CodeChallengeMethod:      "",
		CSRFToken:                "",
		Subject:                  &subject,
		CreatedAt:                now,
		FirstParty:               true,
		AutoConnectDone:          false,
	}
	// The probe logs its own refusals; this only says the verdict did not land.
	if err := s.probeRemoteSession(ctx, logger, endpoint, state, *client, nil, remotesessionmetrics.ValidationTriggerKeepalive); err != nil {
		logger.InfoContext(ctx, "remote session re-check did not record a verdict", attr.SlogError(err))
	}
}

// issuerHost keys the per-host limiter; an unparseable issuer shares one bucket.
func issuerHost(issuerURL string) string {
	parsed, err := url.Parse(issuerURL)
	if err != nil || parsed.Host == "" {
		return "unknown"
	}
	return strings.ToLower(parsed.Host)
}
