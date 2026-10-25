package mcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	attemptStageUpstreamCallback = "upstream_callback"
	attemptStageUpstreamRefresh  = "upstream_refresh"
)

// Provider outcomes. Each counts one logical callback or refresh, not a call
// to the provider: stored means the credential was committed, not that the
// person's browser reached the client; adopted means another request's
// refresh was reused, whether or not that one contacted the provider.
const (
	outcomeStored  attemptOutcome = "stored"
	outcomeRetried attemptOutcome = "retried"
	outcomeAdopted attemptOutcome = "adopted"
	outcomeNoGrant attemptOutcome = "no_grant"
)

const (
	reasonProviderDenied   attemptReason = "provider_denied"
	reasonProviderRejected attemptReason = "provider_rejected"
	reasonResourceRejected attemptReason = "resource_rejected"
	reasonStoreRefused     attemptReason = "store_refused"
	reasonStoreFailed      attemptReason = "store_failed"
	reasonInProgress       attemptReason = "in_progress"
)

// providerUnavailableCodes are the registered OAuth codes by which a provider
// says it could not serve the request, rather than refusing it.
var providerUnavailableCodes = map[string]bool{
	"server_error":            true,
	"temporarily_unavailable": true,
}

// callbackResult classifies a provider callback that consumed its state.
func callbackResult(ctx context.Context, obs *remotesessions.RemoteLoginObservation, err error) inboundResult {
	failed := func(class failureClass, reason attemptReason) inboundResult {
		return inboundResult{outcome: outcomeFailed, class: class, reason: reason}
	}
	switch {
	case err == nil && obs.Phase == remotesessions.RemoteLoginPhaseStored:
		return inboundResult{outcome: outcomeStored, class: classNone, reason: reasonNone}
	case err == nil:
		return inboundResult{outcome: outcomeRetried, class: classNone, reason: reasonNone}
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		return failed(classCanceled, reasonCanceled)
	}
	switch obs.Phase {
	case remotesessions.RemoteLoginPhaseDenied:
		if providerUnavailableCodes[obs.ProviderErrorCode] {
			return failed(classEndpoint, providerOAuthReason(obs.ProviderErrorCode))
		}
		if obs.ProviderErrorCode != "" {
			return failed(classConfiguration, providerOAuthReason(obs.ProviderErrorCode))
		}
		return failed(classConfiguration, reasonProviderDenied)
	case remotesessions.RemoteLoginPhaseRetrying:
		if obs.Failure == remotesessions.RemoteLoginFailureConnectionGone {
			return failed(classConfiguration, attemptReason(obs.Failure))
		}
		// Only a refused retry is the resource's; anything else that stopped
		// minting the retry is classified by its code.
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code == oops.CodeUnauthorized {
			return failed(classConfiguration, reasonResourceRejected)
		}
	case remotesessions.RemoteLoginPhaseExchanging:
		return exchangeResult(obs)
	case remotesessions.RemoteLoginPhaseStoring:
		switch obs.Failure {
		case remotesessions.RemoteLoginFailureUnrenewableToken, remotesessions.RemoteLoginFailureConnectionGone, remotesessions.RemoteLoginFailureLoginExpired:
			return failed(classConfiguration, attemptReason(obs.Failure))
		default:
		}
		if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code == oops.CodeUnauthorized {
			return failed(classConfiguration, reasonStoreRefused)
		}
		return failed(classInternal, reasonStoreFailed)
	case remotesessions.RemoteLoginPhaseValidating, remotesessions.RemoteLoginPhaseStored, remotesessions.RemoteLoginPhaseRetried:
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		return shareableResult(shareable.Code)
	}
	return failed(classInternal, reasonUnexpected)
}

// providerOAuthReason keeps provider OAuth refusals distinct from gateway
// reasons with the same name, such as invalid_request. Codes are allowlisted
// by the remote session package before reaching this boundary.
func providerOAuthReason(code string) attemptReason {
	return attemptReason("provider_oauth_" + code)
}

// exchangeResult separates provider/tunnel communication failures from local
// signing and configuration faults using the remote session package's types.
func exchangeResult(obs *remotesessions.RemoteLoginObservation) inboundResult {
	failed := func(class failureClass, reason attemptReason) inboundResult {
		return inboundResult{outcome: outcomeFailed, class: class, reason: reason}
	}
	switch obs.Failure {
	case remotesessions.RemoteLoginFailureUnreachable, remotesessions.RemoteLoginFailureTunnel,
		remotesessions.RemoteLoginFailureProviderOrTunnel, remotesessions.RemoteLoginFailureRateLimited,
		remotesessions.RemoteLoginFailureUnavailable, remotesessions.RemoteLoginFailureServerError,
		remotesessions.RemoteLoginFailureInvalidResponse:
		return failed(classEndpoint, attemptReason(obs.Failure))
	case remotesessions.RemoteLoginFailureRejected:
		if obs.ProviderErrorCode != "" {
			return failed(classConfiguration, providerOAuthReason(obs.ProviderErrorCode))
		}
		return failed(classConfiguration, reasonProviderRejected)
	case remotesessions.RemoteLoginFailureAssertionKey:
		return failed(classConfiguration, attemptReason(obs.Failure))
	case remotesessions.RemoteLoginFailureSigning:
		return failed(classInternal, attemptReason(obs.Failure))
	case remotesessions.RemoteLoginFailureNone, remotesessions.RemoteLoginFailureInternal,
		remotesessions.RemoteLoginFailureUnrenewableToken, remotesessions.RemoteLoginFailureConnectionGone,
		remotesessions.RemoteLoginFailureLoginExpired:
	}
	return failed(classInternal, reasonUnexpected)
}

// recordRemoteLoginCallback records a provider callback whose state named a
// configured gateway. A callback whose state was missing has no observation,
// and an organization gateway spans projects, so neither is recorded.
func (s *Service) recordRemoteLoginCallback(ctx context.Context, observedAt time.Time, obs *remotesessions.RemoteLoginObservation, err error) {
	if obs == nil || !obs.MetaMcpServerID.Valid || obs.RouteBase == mcproute.OrganizationGateway || obs.ProjectID == uuid.Nil || obs.OrganizationID == "" {
		return
	}
	var subject urn.SessionSubject
	if obs.Subject != nil {
		subject = *obs.Subject
	}
	caller := attemptCaller{
		projectID:           obs.ProjectID,
		organizationID:      obs.OrganizationID,
		gatewayID:           obs.MetaMcpServerID.UUID,
		userSessionIssuerID: uuid.Nil,
		subject:             subject,
		oauthClientID:       "",
		traceID:             traceIDOf(ctx),
	}
	clientKey := inboundUnverifiedClient
	if obs.InboundUserSessionClientID.Valid {
		clientKey = obs.InboundUserSessionClientID.UUID.String()
	}
	result := callbackResult(ctx, obs, err)
	go s.writeRemoteLoginCallback(context.WithoutCancel(ctx), caller, observedAt, *obs, result, clientKey)
}

func (s *Service) writeRemoteLoginCallback(ctx context.Context, caller attemptCaller, observedAt time.Time, obs remotesessions.RemoteLoginObservation, result inboundResult, clientKey string) {
	ctx, cancel := context.WithTimeout(ctx, connectionAttemptWriteTimeout)
	defer cancel()
	provider := obs.ProviderIssuerID
	if !provider.Valid {
		provider = s.remoteLoginProvider(ctx, obs)
	}
	id, err := uuid.NewV7()
	if err != nil {
		s.logger.WarnContext(ctx, "mint provider callback attempt id", attr.SlogError(err))
		return
	}
	attrs := caller.attemptAttributes(id, attemptStageUpstreamCallback, result.outcome, result.class, result.reason, clientKey)
	if provider.Valid {
		attrs[attr.ConnectionAttemptProviderIDKey] = provider.UUID.String()
	}
	if err := s.telemLogger.LogBulkBounded(ctx, []tm.LogParams{caller.attemptRow(observedAt, attrs)}); err != nil {
		s.logger.WarnContext(ctx, "record provider callback attempt", attr.SlogError(err), attr.SlogMetaMcpServerID(caller.gatewayID.String()))
	}
}

// remoteLoginProvider is the provider of a callback that ended before its
// client was loaded, read from the client the login was started for within
// the login's own project.
func (s *Service) remoteLoginProvider(ctx context.Context, obs remotesessions.RemoteLoginObservation) uuid.NullUUID {
	ctx, cancel := context.WithTimeout(ctx, attemptClientLookupTimeout)
	defer cancel()
	row, err := remotesessions_repo.New(s.db).GetRemoteSessionClientByID(ctx, remotesessions_repo.GetRemoteSessionClientByIDParams{
		ID:             obs.RemoteSessionClientID,
		ProjectID:      obs.ProjectID,
		OrganizationID: obs.OrganizationID,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "resolve provider callback provider", attr.SlogError(err))
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	}
	return uuid.NullUUID{UUID: row.RemoteSessionClient.RemoteSessionIssuerID, Valid: true}
}

// refreshResult classifies one request-driven refresh. A refresh still
// running when the request finished is unknown, never a success.
func refreshResult(observation remotesessions.RefreshObservation) inboundResult {
	failed := func(class failureClass) inboundResult {
		return inboundResult{outcome: outcomeFailed, class: class, reason: attemptReason(observation.Outcome)}
	}
	if !observation.Finished {
		return inboundResult{outcome: outcomeUnknown, class: classNone, reason: reasonInProgress}
	}
	switch observation.Outcome {
	case remotesessionmetrics.RefreshOutcomeRefreshed:
		return inboundResult{outcome: outcomeRefreshed, class: classNone, reason: reasonNone}
	case remotesessionmetrics.RefreshOutcomeAdoptedConcurrentWinner:
		return inboundResult{outcome: outcomeAdopted, class: classNone, reason: reasonNone}
	case remotesessionmetrics.RefreshOutcomeNoGrant, remotesessionmetrics.RefreshOutcomeSessionInactive:
		return inboundResult{outcome: outcomeNoGrant, class: classNone, reason: attemptReason(observation.Outcome)}
	case remotesessionmetrics.RefreshOutcomeInvalidGrant, remotesessionmetrics.RefreshOutcomeInvalidClient,
		remotesessionmetrics.RefreshOutcomeRejected, remotesessionmetrics.RefreshOutcomeRejectedUnparsed:
		return failed(classConfiguration)
	case remotesessionmetrics.RefreshOutcomeUpstreamError, remotesessionmetrics.RefreshOutcomeRateLimited,
		remotesessionmetrics.RefreshOutcomeUnreachable:
		return failed(classEndpoint)
	case remotesessionmetrics.RefreshOutcomeCanceled:
		return failed(classCanceled)
	case remotesessionmetrics.RefreshOutcomeInternalError:
		return failed(classInternal)
	}
	return inboundResult{outcome: outcomeFailed, class: classInternal, reason: reasonUnexpected}
}

// observeGatewayRefreshes makes the refreshes a configured gateway request
// causes report to it, and returns the function that records them once the
// request is served. Recording never waits on telemetry.
func (s *Service) observeGatewayRefreshes(ctx context.Context, caller attemptCaller) (context.Context, func()) {
	sink := remotesessions.NewRefreshObservations(tm.ConnectionAttemptMaxRefreshesPerRequest)
	finish := func() {
		observations, dropped := sink.Close()
		if len(observations) == 0 && dropped == 0 {
			return
		}
		go s.writeGatewayRefreshes(context.WithoutCancel(ctx), caller, observations, dropped)
	}
	return remotesessions.WithRefreshObservations(ctx, sink), finish
}

func (s *Service) writeGatewayRefreshes(ctx context.Context, caller attemptCaller, observations []remotesessions.RefreshObservation, dropped int) {
	ctx, cancel := context.WithTimeout(ctx, connectionAttemptWriteTimeout)
	defer cancel()
	if dropped > 0 {
		s.logger.WarnContext(ctx, "gateway request made more provider refreshes than are recorded", attr.SlogMetaMcpServerID(caller.gatewayID.String()), attr.SlogValueInt(dropped))
	}
	if len(observations) == 0 {
		return
	}
	rows, err := refreshRows(caller, observations, s.attemptClientKey(ctx, caller))
	if err != nil {
		s.logger.WarnContext(ctx, "mint provider refresh attempt id", attr.SlogError(err))
		return
	}
	if err := s.telemLogger.LogBulkBounded(ctx, rows); err != nil {
		s.logger.WarnContext(ctx, "record provider refresh attempts", attr.SlogError(err), attr.SlogMetaMcpServerID(caller.gatewayID.String()))
	}
}

// refreshRows is one attempt row per recorded refresh, and nothing else.
func refreshRows(caller attemptCaller, observations []remotesessions.RefreshObservation, clientKey string) ([]tm.LogParams, error) {
	rows := make([]tm.LogParams, 0, len(observations))
	for _, observation := range observations {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("mint attempt id: %w", err)
		}
		result := refreshResult(observation)
		attrs := caller.attemptAttributes(id, attemptStageUpstreamRefresh, result.outcome, result.class, result.reason, clientKey)
		if observation.ProviderIssuerID.Valid {
			attrs[attr.ConnectionAttemptProviderIDKey] = observation.ProviderIssuerID.UUID.String()
		}
		rows = append(rows, caller.attemptRow(observation.Started, attrs))
	}
	return rows, nil
}

// gatewayRefreshCaller is who a configured gateway request's refreshes are
// recorded for, once the issuer gate has authenticated them.
func gatewayRefreshCaller(ctx context.Context, projectID uuid.UUID, organizationID string, gatewayID, userSessionIssuerID uuid.UUID, authentication *issuerGateAuthentication) attemptCaller {
	oauthClientID, _ := contextvalues.GetOAuthClientID(ctx)
	return attemptCaller{
		projectID:           projectID,
		organizationID:      organizationID,
		gatewayID:           gatewayID,
		userSessionIssuerID: userSessionIssuerID,
		subject:             authentication.subject,
		oauthClientID:       oauthClientID,
		traceID:             traceIDOf(ctx),
	}
}

func traceIDOf(ctx context.Context) string {
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.HasTraceID() {
		return spanCtx.TraceID().String()
	}
	return ""
}
