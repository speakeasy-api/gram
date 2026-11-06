// Live validation for the consent page: probe the routed credential upstream and record the verdict.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/mcp/tunnelrouting"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/mcpconnectionattempts"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/oauthui"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/ratelimit"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	"github.com/speakeasy-api/gram/server/internal/remotemcp/proxy"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/remotesessionmetrics"
	remotesessions_repo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// errRemoteSessionUnroutable marks a credential no upstream on this endpoint is ever handed.
var errRemoteSessionUnroutable = errors.New("remote session routes to no upstream on this endpoint")

// errRemoteSessionNoUsableCredential marks a card whose grant cannot produce a token right now, so nothing was probed.
var errRemoteSessionNoUsableCredential = errors.New("remote session has no usable credential")

// errRemoteSessionVerdictNotRecorded: the upstream was dialled but the verdict could not be written.
var errRemoteSessionVerdictNotRecorded = errors.New("remote session verdict not recorded")

// errRemoteSessionMemberOffline: a keepalive found no live tunnel route; no verdict is written so a reconnect is judged promptly.
var errRemoteSessionMemberOffline = errors.New("remote session member tunnel has no live route")

// errValidationRateLimited marks a verify that ran no probe: rate limited, or another refresh is running.
var errValidationRateLimited = errors.New("remote session validation rate limited")

// validationRate caps verifies per consent challenge.
var validationRate = ratelimit.PerMinute(6)

// errValidationUnfinished marks a verify an outage stopped; the connect page reports it.
var errValidationUnfinished = errors.New("remote session validation did not finish")

// verifyHitStoreOutage rules out the caller's own deadline, which is no outage.
func verifyHitStoreOutage(ctx context.Context, err error) bool {
	return ctx.Err() == nil && oops.IsInfrastructureFailure(err)
}

// checkFailure is what a check that reached no verdict means. Its row and its
// error page both read it, so they agree on who fixes it and whether retrying helps.
type checkFailure struct {
	// code is the row's reason.
	code mcpconnectionattempts.Code

	// page explains it on the error page; ReasonDefault when the card shows a notice instead.
	page oauthui.Reason
}

// noUsableCredentialError is a card whose grant yielded no token. cause is why,
// nil when the card is not connected or its grant expired.
type noUsableCredentialError struct {
	cause error
}

func (e noUsableCredentialError) Error() string { return errRemoteSessionNoUsableCredential.Error() }

func (e noUsableCredentialError) Unwrap() []error {
	return []error{errRemoteSessionNoUsableCredential, e.cause}
}

// classifyCheckFailure explains a check that reached no verdict; false when it records nothing.
func classifyCheckFailure(ctx context.Context, err error) (checkFailure, bool) {
	none := checkFailure{code: mcpconnectionattempts.Code{}, page: oauthui.ReasonDefault}
	switch {
	case err == nil, ctx.Err() != nil,
		errors.Is(err, errValidationRateLimited), errors.Is(err, errRemoteSessionMemberOffline), errors.Is(err, errRemoteSessionVerdictNotRecorded),
		remotesessions.RefreshFailureRecorded(err), remotesessions.RefreshStepRecorded(err):
		// Nothing was checked, the request ended, or another row already says what happened.
		return none, false
	case errors.Is(err, errRemoteSessionNoUsableCredential):
		code := noUsableCredentialCode(ctx, err)
		return checkFailure{code: code, page: oauthui.ReasonDefault}, code != (mcpconnectionattempts.Code{})
	case errors.Is(err, errRemoteSessionUnroutable):
		return checkFailure{code: mcpconnectionattempts.CodeCredentialMisconfigured, page: oauthui.ReasonCheckNeedsAdmin}, true
	}
	code := checkFailureCode(ctx, err)
	// A refresh another request holds is that request's row.
	if code == (mcpconnectionattempts.Code{}) {
		return none, false
	}
	return checkFailure{code: code, page: checkFailurePage(code)}, true
}

// checkFailureCode names the cause of a failed check: a store outage, a credential or member failure, or our own fault.
func checkFailureCode(ctx context.Context, err error) mcpconnectionattempts.Code {
	// Access to the server, or to the connection it uses, is the admin's to grant.
	if mcpaccess.IsServerPermissionDenied(err) || errors.Is(err, errAuthClientNoAccess) {
		return mcpconnectionattempts.CodeNoServerAccess
	}
	// Any other refusal is of the caller's identity: credential admission refused it.
	if refusal, ok := errors.AsType[*oops.ShareableError](err); ok && (refusal.Code == oops.CodeForbidden || refusal.Code == oops.CodeUnauthorized) {
		return mcpconnectionattempts.CodeSubjectNotAdmitted
	}
	code := resolveFailureHappened(err)
	if member, ok := errors.AsType[memberConnectionError](err); ok {
		code = member.code
	}
	if code == mcpconnectionattempts.CodeInternalError && verifyHitStoreOutage(ctx, err) {
		code = mcpconnectionattempts.CodeAuthCheckUnavailable
	}
	return code
}

// checkFailureBrowserError explains a failed check on the error page the way its row records it.
func checkFailureBrowserError(ctx context.Context, err error) error {
	if failure, ok := classifyCheckFailure(ctx, err); ok && failure.page != oauthui.ReasonDefault {
		return fmt.Errorf("check connection: %w", oops.WithBrowserReason(err, failure.page))
	}
	return err
}

// checkFailurePage suggests retrying only for our outage. A party no failed check reaches this page for gets the generic page.
func checkFailurePage(code mcpconnectionattempts.Code) oauthui.Reason {
	if code == mcpconnectionattempts.CodeNoServerAccess || code == mcpconnectionattempts.CodeSubjectNotAdmitted {
		return oauthui.ReasonCheckNotAllowed
	}
	meaning, _ := mcpconnectionattempts.MeaningOf(code.String())
	switch meaning.WhoFixes {
	case mcpconnectionattempts.WhoAdmin:
		return oauthui.ReasonCheckNeedsAdmin
	case mcpconnectionattempts.WhoSpeakeasy:
		if code == mcpconnectionattempts.CodeInternalError {
			return oauthui.ReasonCheckFailed
		}
		return oauthui.ReasonCheckUnavailable
	case mcpconnectionattempts.WhoProvider, mcpconnectionattempts.WhoPerson, mcpconnectionattempts.WhoApp, mcpconnectionattempts.WhoNobody:
		// Provider outages show on the card; no failed check reaches this page for these parties.
		return oauthui.ReasonDefault
	default:
		return oauthui.ReasonDefault
	}
}

// validationUnfinishedNotice follows a Verify that reached no verdict; it says nothing about now.
const validationUnfinishedNotice = "Your last Verify didn't finish. Try again shortly. If it keeps happening, ask your administrator to contact Speakeasy support."

// validationLimitedNotice is the fixed card copy for a rate-limited verify.
const validationLimitedNotice = "Try again in a moment"

// refreshUnavailableNotice follows a Refresh that didn't finish; it says nothing about now.
const refreshUnavailableNotice = "Your last refresh didn't finish. Try again shortly."

// newValidationLimiter builds the per-challenge verify limiter; nil without Redis.
func newValidationLimiter(redisClient *redis.Client, meterProvider metric.MeterProvider) *ratelimit.Limiter {
	if redisClient == nil {
		return nil
	}
	return ratelimit.New(ratelimit.NewRedisStore(redisClient), "remote_session_validate", validationRate, ratelimit.WithMetrics(meterProvider))
}

// validationTarget is the upstream a credential is presented to: the name a verdict quotes, and the builder that dials it.
type validationTarget struct {
	name  string
	build memberProxyBuilder
	// tunnelID is set when the upstream is reached through a tunnel, so a keepalive can skip a dial that has no route.
	tunnelID uuid.NullUUID
	// memberID is the gateway member dialled, nil on a direct server.
	memberID uuid.UUID
	// setupFailed means routing failed before the provider could be contacted.
	setupFailed bool
}

// validateRemoteSession probes one card's grant through the runtime routing path and records the verdict.
func (s *Service) validateRemoteSession(
	ctx context.Context,
	r *http.Request,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	client remotesessions.Client,
) error {
	if s.validationLimiter != nil {
		res, lerr := s.validationLimiter.Allow(ctx, challengeState.ID)
		switch {
		case lerr != nil:
			logger.WarnContext(ctx, "remote session validation limiter unavailable; allowing", attr.SlogError(lerr))
		case !res.Allowed:
			logger.InfoContext(ctx, "remote session validation rate limited")
			return errValidationRateLimited
		}
	}
	return s.probeRemoteSession(ctx, consentRefreshAttribution(r, endpoint, challengeState), logger, mcpconnectionattempts.FromRequest(r), endpoint, challengeState, client, nil, remotesessionmetrics.ValidationTriggerVerify)
}

// probeRemoteSession presents the card's credential to its upstream and records
// the verdict. Callers pace it: the consent action through the per-challenge
// limiter, the connect path by running it once per committed grant, the
// keepalive through its claim lease and per-host limiter.
func (s *Service) probeRemoteSession(
	ctx context.Context,
	attribution remotesessions.RefreshAttribution,
	logger *slog.Logger,
	fromRequest mcpconnectionattempts.Option,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	client remotesessions.Client,
	expectedGrant *remotesessions.RemoteGrant,
	trigger remotesessionmetrics.ValidationTrigger,
) (retErr error) {
	subject := *challengeState.Subject
	logger = logger.With(attr.SlogRemoteSessionClientID(client.ID.String()))
	skipped := new(remotesessions.SkippedClients)
	attribution = attribution.NotingSkips(skipped)
	defer func() {
		if failure, ok := classifyCheckFailure(ctx, retErr); ok {
			s.recordRemoteSessionCheck(ctx, fromRequest, endpoint, challengeState, client, trigger, failure.code)
		}
	}()

	tokenProjectIDs, err := s.clientTokenProjectIDs(ctx, endpoint, client)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "resolve upstream token projects for validation").LogError(ctx, logger)
	}
	var (
		tokens         map[uuid.UUID]remotesessions.UpstreamToken
		entry          remotesessions.UpstreamToken
		tokenProjectID uuid.UUID
		usable         bool
	)
	for _, projectID := range tokenProjectIDs {
		resolved, rerr := s.remoteChallengeMgr.ResolveAvailableAccessTokens(ctx, attribution, projectID, endpoint.OrganizationID, endpoint.UserSessionIssuerID, subject)
		if verifyHitStoreOutage(ctx, rerr) {
			logger.ErrorContext(ctx, "verify stopped by a store outage", attr.SlogError(rerr))
			return fmt.Errorf("%w: %w", errValidationUnfinished, rerr)
		}
		if rerr != nil {
			return oops.E(oops.CodeUnexpected, rerr, "resolve upstream tokens for validation").LogError(ctx, logger)
		}
		if candidate, ok := resolved[client.RemoteSessionIssuerID]; ok && candidate.RemoteSessionClientID == client.ID {
			// Nothing stored can be read, so it checks as not connected.
			if candidate.SignInUnreadable() {
				continue
			}
			if candidate.SessionUnavailable() {
				if remotesessions.RefreshBusy(candidate.ClientCredentialErr) {
					return errValidationRateLimited
				}
				if remotesessions.OutageKindOf(candidate.ClientCredentialErr) == remotesessions.RefreshOutageNone {
					logger.ErrorContext(ctx, "verify stopped by a refresh failure", attr.SlogError(candidate.ClientCredentialErr))
				} else {
					logger.WarnContext(ctx, "verify stopped by an outage", attr.SlogError(candidate.ClientCredentialErr))
				}
				return fmt.Errorf("%w: %w", errValidationUnfinished, candidate.ClientCredentialErr)
			}
			tokens, entry, tokenProjectID, usable = resolved, candidate, projectID, true
			break
		}
	}
	if !usable {
		linked, err := s.clientLinkedToSignIn(ctx, endpoint, client, tokenProjectIDs)
		if err != nil {
			return oops.E(oops.CodeUnexpected, err, "check remote session client binding for validation").LogError(ctx, logger)
		}
		if reason := unservedCredentialReason(len(tokenProjectIDs), linked); reason != "" {
			return oops.E(oops.CodeBadRequest, errRemoteSessionUnroutable, "This connection can't be checked here.").LogWarn(ctx, logger, attr.SlogReason(reason))
		}
		return s.noUsableCredentialRefusal(ctx, logger, endpoint, subject, client, skipped, tokenProjectIDs)
	}
	if expectedGrant != nil && expectedGrant.RemoteSessionID != uuid.Nil &&
		(entry.RemoteSessionID != expectedGrant.RemoteSessionID || !entry.RemoteSessionResolvedFromUpdatedAt.Equal(expectedGrant.RemoteSessionUpdatedAt)) {
		logger.InfoContext(ctx, "new remote grant not verified: credential changed before probe")
		return nil
	}
	// Route the complete credential set, just as serving does. The target resolver
	// separately proves that this card is the credential selected for the target.
	probeCtx, target, err := s.resolveValidationTarget(ctx, logger, endpoint, challengeState, client, tokens)
	switch {
	case errors.Is(err, errRemoteSessionUnroutable):
		return oops.E(oops.CodeBadRequest, err, "This connection is not used by any server behind this endpoint, so it cannot be verified.").LogWarn(ctx, logger)
	case err != nil:
		if _, ok := errors.AsType[*oops.ShareableError](err); ok {
			return err
		}
		return oops.E(oops.CodeUnexpected, err, "resolve validation target").LogError(ctx, logger)
	}

	// A keepalive never dials a tunnel with no live route, and records nothing: the member is offline, not answering.
	if trigger == remotesessionmetrics.ValidationTriggerKeepalive && target.tunnelID.Valid && !s.tunnelHasRoute(probeCtx, target.tunnelID.UUID) {
		return fmt.Errorf("%w: %s", errRemoteSessionMemberOffline, target.name)
	}

	// Only routable, online grants spend the fleet-wide host quota.
	if trigger == remotesessionmetrics.ValidationTriggerKeepalive {
		if err := s.admitRemoteSessionRecheck(ctx, logger, client.IssuerURL); err != nil {
			return err
		}
	}

	ref := remotesessions.RemoteSessionRef{
		ID:             entry.RemoteSessionID,
		Subject:        subject,
		ClientID:       client.ID,
		UpdatedAt:      entry.RemoteSessionUpdatedAt,
		ProjectID:      tokenProjectID,
		OrganizationID: endpoint.OrganizationID,
	}
	// The issuer's own interfaces run beside the probe, under their own budget;
	// only they can say whether the provider still honours the token at all.
	var upstream remotesessions.UpstreamVerification
	enriched := make(chan struct{})
	go func() {
		defer close(enriched)
		var err error
		if upstream, err = s.remoteChallengeMgr.EnrichRemoteSession(ctx, ref); err != nil {
			logger.WarnContext(ctx, "remote session enrichment failed", attr.SlogError(err))
		}
	}()
	probedAt := time.Now()
	verdict, reason := s.probeUpstream(probeCtx, logger, target.build, target.name)
	<-enriched
	ownResource := s.newResourceDisplayOwner(endpoint, challengeState).ownsOrFallsBack(ctx, logger, client)
	issuerDisplay, _ := issuerCardBranding(client, ownResource, s.serverURL)
	verdict, reason = combineUpstreamVerdict(verdict, reason, upstream, issuerDisplay)
	logger = logger.With(attr.SlogRemoteSessionID(entry.RemoteSessionID.String()), attr.SlogOutcome(string(verdict)), attr.SlogOAuthValidationTrigger(string(trigger)))
	s.validationMetrics.Record(ctx, client.IssuerURL, trigger, string(verdict))
	s.recordRemoteSessionVerdict(ctx, fromRequest, endpoint, challengeState, client, target, entry.RemoteSessionID, verdict, trigger)

	written, err := s.remoteChallengeMgr.RecordRemoteSessionValidation(ctx, ref, remotesessions.RemoteSessionValidation{
		Status: verdict,
		Reason: reason,
		At:     probedAt,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", errRemoteSessionVerdictNotRecorded, oops.E(oops.CodeUnexpected, err, "record remote session validation").LogError(ctx, logger))
	}
	if !written {
		logger.InfoContext(ctx, "remote session validation dropped; grant changed or verdict superseded during the probe")
		return nil
	}
	logger.InfoContext(ctx, "remote session validated", attr.SlogReason(reason))
	return nil
}

// clientLinkedToSignIn reports whether token resolution can reach client: a typed registration need not be linked to the sign-in.
func (s *Service) clientLinkedToSignIn(ctx context.Context, endpoint *ResolvedMcpEndpoint, client remotesessions.Client, projectIDs []uuid.UUID) (bool, error) {
	for _, projectID := range projectIDs {
		linked, err := remotesessions_repo.New(s.db).CheckRemoteSessionClientBindingForUserSessionIssuer(ctx, remotesessions_repo.CheckRemoteSessionClientBindingForUserSessionIssuerParams{
			RemoteSessionClientID: client.ID,
			UserSessionIssuerID:   endpoint.UserSessionIssuerID,
			ProjectID:             projectID,
			OrganizationID:        endpoint.OrganizationID,
		})
		if err != nil {
			return false, fmt.Errorf("check remote session client binding: %w", err)
		}
		if linked {
			return true, nil
		}
	}
	return false, nil
}

// unservedCredentialReason names why token resolution can't reach this client; empty when it can.
func unservedCredentialReason(projects int, linked bool) string {
	switch {
	case projects == 0:
		return "this client serves no project on this endpoint"
	case !linked:
		return "this client is not linked to the endpoint's sign-in"
	default:
		return ""
	}
}

// noUsableCredentialRefusal refuses a check whose grant yielded no token. A
// store outage reading the card stops the check like an outage; any other read
// failure is ours. Otherwise the refusal carries token resolution's own reason,
// or none when it gave none (the person reconnects).
func (s *Service) noUsableCredentialRefusal(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	subject urn.SessionSubject,
	client remotesessions.Client,
	skipped *remotesessions.SkippedClients,
	projectIDs []uuid.UUID,
) error {
	statuses, err := s.endpointRemoteSessionStatuses(ctx, subject, endpoint)
	if verifyHitStoreOutage(ctx, err) {
		logger.ErrorContext(ctx, "verify stopped by a store outage", attr.SlogError(err))
		return fmt.Errorf("%w: read remote session status: %w", errValidationUnfinished, err)
	}
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "read remote session status for validation").LogError(ctx, logger)
	}
	state, connected := statuses[client.ID]
	reason := describeNoUsableCredential(state, connected, time.Now())
	var cause error
	// Token resolution's own reason wins: it carries whether a refresh already wrote this failure's row.
	for _, projectID := range projectIDs {
		if skip := skipped.Reason(projectID, client.RemoteSessionIssuerID, uuid.NullUUID{UUID: client.ID, Valid: true}); skip != nil {
			cause = fmt.Errorf("skipped during token resolution: %w", skip)
			break
		}
	}
	return oops.E(oops.CodeBadRequest, noUsableCredentialError{cause: cause}, "%s", reason).LogWarn(ctx, logger, attr.SlogReason(reason))
}

// noWorkingSignInMessage covers a broken client setup or an unknown cause: token resolution hides which.
const noWorkingSignInMessage = "We could not get a working sign-in for this service. Try again. If it keeps failing, disconnect it and connect again, or ask your administrator."

// verifyNotice selects the card notice after a Verify that found no usable credential.
type verifyNotice string

const (
	// verifyNoticeSignIn is the default: a broken client setup or an unknown cause.
	verifyNoticeSignIn verifyNotice = ""
	// verifyNoticeReconnect is a grant the person replaces by connecting again.
	verifyNoticeReconnect verifyNotice = "reconnect"
)

// verifyNoticeMessage is the card text for a notice; an unknown value reads as the default.
func verifyNoticeMessage(notice string) string {
	switch verifyNotice(notice) {
	case verifyNoticeReconnect:
		// Shown only on a connected card, which offers Disconnect, not Reconnect.
		return "Disconnect this service, then connect it again."
	case verifyNoticeSignIn:
		return noWorkingSignInMessage
	default:
		return noWorkingSignInMessage
	}
}

// noUsableCredentialCode is the row's code for a check refused for no usable credential.
func noUsableCredentialCode(ctx context.Context, err error) mcpconnectionattempts.Code {
	if refusal, ok := errors.AsType[noUsableCredentialError](err); ok && refusal.cause != nil {
		return checkFailureCode(ctx, refusal.cause)
	}
	return mcpconnectionattempts.CodeInvalidRemoteSession
}

// noUsableCredentialNotice picks the card notice from the same code the row records.
func noUsableCredentialNotice(ctx context.Context, err error) verifyNotice {
	code := noUsableCredentialCode(ctx, err)
	if meaning, _ := mcpconnectionattempts.MeaningOf(code.String()); meaning.WhoFixes == mcpconnectionattempts.WhoPerson {
		return verifyNoticeReconnect
	}
	return verifyNoticeSignIn
}

// describeNoUsableCredential is the log reason for a grant that yielded no token.
func describeNoUsableCredential(state remotesessions.RemoteSessionState, connected bool, now time.Time) string {
	lapsed := func(deadline *time.Time) bool { return deadline != nil && !deadline.After(now) }
	switch {
	case !connected:
		return "no grant is stored for this subject and client"
	case lapsed(state.AuthorizationExpiresAt):
		return "grant authorization lifetime has ended"
	case lapsed(state.RefreshExpiresAt):
		return "grant refresh token lifetime has ended"
	case lapsed(state.AccessExpiresAt) && !state.CanRefresh:
		return "access token has expired and the grant has no refresh token"
	default:
		return "grant is stored but its token could not be obtained or refreshed"
	}
}

// resolveValidationTarget finds the upstream this endpoint would hand the credential to, judged as the consent subject.
func (s *Service) resolveValidationTarget(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	client remotesessions.Client,
	tokens map[uuid.UUID]remotesessions.UpstreamToken,
) (context.Context, validationTarget, error) {
	var none validationTarget
	subject := *challengeState.Subject
	sessionID := "consent:" + challengeState.ID
	ctx, err := s.contextForSessionSubject(ctx, endpoint, subject, sessionID, challengeState.ClientID)
	if err != nil {
		return ctx, none, fmt.Errorf("stamp consent subject context: %w", err)
	}

	ctx = s.consentDiscoveryContext(ctx, challengeState)

	switch {
	case endpoint.MetaMcpServerID.Valid:
		return s.metaValidationTarget(ctx, logger, endpoint, client, tokens, sessionID, subject)
	case endpoint.McpServerID.Valid:
		return s.standaloneValidationTarget(ctx, logger, endpoint, challengeState, client, tokens)
	default:
		return ctx, none, errRemoteSessionUnroutable
	}
}

// metaValidationTarget dials the member the grant is qualified to, through the gateway's own routing.
func (s *Service) metaValidationTarget(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	client remotesessions.Client,
	tokens map[uuid.UUID]remotesessions.UpstreamToken,
	sessionID string,
	subject urn.SessionSubject,
) (context.Context, validationTarget, error) {
	var none validationTarget
	rows, claimed, err := s.claimingMetaMembers(ctx, endpoint, client)
	if err != nil {
		return ctx, none, fmt.Errorf("resolve claiming meta MCP members: %w", err)
	}
	if !claimed {
		return ctx, none, errRemoteSessionUnroutable
	}
	entry := tokens[client.RemoteSessionIssuerID]
	targetID := uuid.Nil
	for _, row := range rows {
		// The dispatch selectors: a tunneled member takes only its own issuer's entry (tunneledIssuerToken).
		routes := grantRoutesToUpstream(entry.Resource, row.UpstreamUrl, false)
		if row.Tunneled {
			routed, err := tunneledIssuerToken(tokens, conv.ToNullUUID(client.RemoteSessionIssuerID), row.UpstreamUrl)
			routes = err == nil && routed.Token != ""
		}
		if routes {
			targetID = row.McpServerID
			break
		}
	}
	if targetID == uuid.Nil {
		return ctx, none, errRemoteSessionUnroutable
	}

	ctx, members, err := s.resolveEndpointMemberSnapshot(ctx, logger, mandatorySurfaceConsent, endpoint)
	if err != nil {
		return ctx, none, fmt.Errorf("resolve meta MCP member snapshot: %w", err)
	}
	var member metaMember
	found := false
	for _, candidate := range members {
		if candidate.serverID == targetID {
			member = candidate
			found = true
			break
		}
	}
	if !found {
		return ctx, none, errRemoteSessionUnroutable
	}
	if member.associatedClients != nil {
		// The member claimed this client by association, so a credential that
		// chose it routes by the client's own issuer.
		member.remoteSessionIssuerID = uuid.NullUUID{UUID: client.RemoteSessionIssuerID, Valid: true}
	}
	name := conv.Default(member.name, member.slug)

	selectedTokens := map[uuid.UUID]remotesessions.UpstreamToken{
		client.RemoteSessionIssuerID: entry,
	}
	// No inbound negotiation occurred; the SDK chooses the outbound revision.
	gate := metaGateContext{
		fromRequest:      mcpconnectionattempts.FromRequest(nil),
		skippedUpstreams: nil,
		request:          nil,
		discoveryMode:    "progressive",
		projectID:        endpoint.ProjectID,
		metaServerID:     endpoint.MetaMcpServerID.UUID,
		// Agent keys reach no consent page.
		agentID:             uuid.Nil,
		organizationGateway: endpoint.IsOrganizationGateway(),
		organizationID:      endpoint.OrganizationID,
		// The probe routes exactly the grant being validated, whatever the
		// gateway kind.
		tokens:        selectedTokens,
		projectTokens: nil,
		memberChoices: nil,
		// Consent probes never acquire credentials by identity chaining.
		userSessionIssuerID: uuid.Nil,
		connectRoot:         "",
		chainUpstream:       nil,
		catalog:             nil,
		consentDiagnostics:  nil,
		catalogRecoveryOnly: false,
		catalogRef:          nil,
		authentication:      nil,
		unapprovedMembers:   nil,
		hiddenMembers:       nil,
		deniedMembers:       nil,
		execute:             false,
		authenticated:       subject.Kind != urn.SessionSubjectKindAnonymous,
		sessionID:           sessionID,
		chatID:              "",
		userID:              "",
		externalUserID:      "",
		apiKeyID:            "",
		protocolVersion:     mcpversions.Resolution{Declared: "", InEffect: mcpversions.DefaultInEffect},
		// Consent probes never call connection_status.
		connectURL:        "",
		agentFix:          nil,
		skillsOverMCP:     nil,
		servedCredentials: nil,
	}
	switch subject.Kind {
	case urn.SessionSubjectKindUser:
		gate.userID = subject.ID
	case urn.SessionSubjectKindAPIKey:
		gate.apiKeyID = subject.ID
	case urn.SessionSubjectKindAgent, urn.SessionSubjectKindAnonymous, urn.SessionSubjectKindWorkload:
		// The agent or workload actor rides on ctx; the gate has no field for it.
	}
	ctx = s.memberAttributionContext(ctx, logger, &gate)
	// routeMetaMember, not dialMetaMember: a probe is not a dispatch to count.
	dial, _, err := s.routeMetaMember(ctx, logger, gate, member, gate.callerIdentity(), false)
	if err != nil {
		if errors.Is(err, errAmbiguousMemberCredential) {
			return ctx, none, fmt.Errorf("%w: %w", errRemoteSessionUnroutable, err)
		}
		if memberErr, ok := errors.AsType[*metaMemberError](err); ok {
			// An unservable member is the probe's failure to report, not a reason to refuse the check.
			return ctx, validationTarget{name: name, build: func(context.Context) (*proxy.Proxy, error) { return nil, memberErr }, tunnelID: member.tunneledServerID, memberID: member.serverID, setupFailed: true}, nil
		}
		return ctx, none, fmt.Errorf("dial meta MCP member: %w", err)
	}
	if dial.anonymous {
		return ctx, none, errRemoteSessionUnroutable
	}
	// The dial proves this card matches the actual backend. Check the complete
	// set for ambiguity without loading and authorizing that backend twice.
	if _, err := routeMetaMemberToken(tokens, member, entry.Resource); err != nil {
		return ctx, none, fmt.Errorf("%w: %w", errRemoteSessionUnroutable, err)
	}
	return ctx, validationTarget{name: name, build: probeProxyBuilder(dial.build), tunnelID: member.tunneledServerID, memberID: member.serverID, setupFailed: false}, nil
}

// standaloneValidationTarget dials a proxied endpoint's own backend, routed as serveRemoteBackend and serveTunneledBackend route it.
func (s *Service) standaloneValidationTarget(
	ctx context.Context,
	logger *slog.Logger,
	endpoint *ResolvedMcpEndpoint,
	challengeState AuthnChallengeState,
	client remotesessions.Client,
	tokens map[uuid.UUID]remotesessions.UpstreamToken,
) (context.Context, validationTarget, error) {
	var none validationTarget
	server, err := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
		ID:        endpoint.McpServerID.UUID,
		ProjectID: endpoint.ProjectID,
	})
	if err != nil {
		return ctx, none, fmt.Errorf("load mcp server for validation: %w", err)
	}
	if !server.RemoteMcpServerID.Valid && !server.TunneledMcpServerID.Valid {
		return ctx, none, errRemoteSessionUnroutable
	}

	// A credential that chose this client routes only its token, by the
	// client's own issuer.
	backendIssuer := uuid.NullUUID{UUID: client.RemoteSessionIssuerID, Valid: true}
	selected := tokens[client.RemoteSessionIssuerID]
	selectedTokens := map[uuid.UUID]remotesessions.UpstreamToken{client.RemoteSessionIssuerID: selected}
	selectedToken, err := routeUpstreamToken(ctx, logger, selectedTokens, endpoint.UpstreamResource, server.TunneledMcpServerID.Valid, backendIssuer)
	var routeErr *upstreamRoutingError
	switch {
	case errors.As(err, &routeErr), selectedToken.Token == "":
		return ctx, none, errRemoteSessionUnroutable
	case err != nil:
		return ctx, none, fmt.Errorf("route selected upstream token for validation: %w", err)
	}
	token := selectedToken.Token

	authorized, err := s.authorizeProxyBackendAccess(ctx, logger, mandatorySurfaceConsent, endpoint.OrganizationID, endpoint.ProjectID, &server)
	if err != nil {
		return ctx, none, fmt.Errorf("authorize validation access: %w", err)
	}
	ctx = authorized
	name := conv.Default(server.Name.String, endpoint.Slug)

	if server.RemoteMcpServerID.Valid {
		// The backend rows load here, outside the probe budget.
		build, berr := s.remoteBackendProxyBuilder(ctx, logger, endpoint.ProjectID, endpoint.OrganizationID, &server, token, "", nil, remotemcp.WithoutToolsCallIdentityCoverage())
		if berr != nil {
			return ctx, none, fmt.Errorf("build remote backend proxy for validation: %w", berr)
		}
		return ctx, validationTarget{name: name, build: probeProxyBuilder(build), tunnelID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, memberID: uuid.Nil, setupFailed: false}, nil
	}
	// One state-derived affinity key pins the handshake and its close to a single gateway.
	affinity := tunnelrouting.HashedClientAffinityKey("consent-validate", challengeState.ID)
	return ctx, validationTarget{name: name, build: probeProxyBuilder(func(ctx context.Context) (*proxy.Proxy, error) {
		p, berr := s.tunnelManager.buildProxy(ctx, logger, buildProxyParams{
			ClientAffinityKey:  affinity,
			ProjectID:          endpoint.ProjectID,
			OrganizationID:     endpoint.OrganizationID,
			MCPServer:          &server,
			ResourceIdentifier: endpoint.UpstreamResource,
			UpstreamAuth:       token,
			WWWAuthenticate:    "",
			Selection:          nil,
			Request:            nil,
		}, remotemcp.WithoutToolsCallIdentityCoverage())
		if berr != nil {
			return nil, fmt.Errorf("build tunnel proxy: %w", berr)
		}
		return p, nil
	}), tunnelID: server.TunneledMcpServerID, memberID: uuid.Nil, setupFailed: false}, nil
}

// tunnelHasRoute is one route-store read: whether any gateway currently holds the tunnel; a store error reads as no route.
func (s *Service) tunnelHasRoute(ctx context.Context, tunnelID uuid.UUID) bool {
	// No route store means no gateway can be dialled, so the member reads offline.
	if s.tunnelManager == nil || s.tunnelManager.routes == nil {
		return false
	}
	candidates, err := s.tunnelManager.routes.Candidates(ctx, tunnelID.String())
	return err == nil && len(candidates) > 0
}

// probeProxyBuilder strips client-session and census interceptors and proxy
// metrics so a synthetic validation handshake is not counted as user traffic.
func probeProxyBuilder(build memberProxyBuilder) memberProxyBuilder {
	return func(ctx context.Context) (*proxy.Proxy, error) {
		p, err := build(ctx)
		if err != nil {
			return nil, err
		}
		p.InitializeRequestInterceptors = nil
		p.UserRequestObservationInterceptors = nil
		p.Metrics = nil
		p.RequestObserver = nil
		return p, nil
	}
}

// combineUpstreamVerdict folds the issuer's introspection into the member's
// verdict. A member that accepted the token is trusted over the provider; any
// other verdict yields to an inactive answer, which is authoritative evidence
// that the token is dead (RFC 7662: expired, revoked, or not introspectable).
func combineUpstreamVerdict(verdict remotesessions.ValidationOutcome, reason string, upstream remotesessions.UpstreamVerification, issuer string) (remotesessions.ValidationOutcome, string) {
	if verdict == remotesessions.ValidationOutcomeValid || !upstream.Inactive {
		return verdict, reason
	}
	return remotesessions.ValidationOutcomeInactive, inactiveReason(issuer)
}

func inactiveReason(issuer string) string {
	return "Inactive at " + issuer
}
