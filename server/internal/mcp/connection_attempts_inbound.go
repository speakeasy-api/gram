package mcp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oops"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

const (
	attemptStageInboundAuthorize = "inbound_authorize"
	attemptStageInboundToken     = "inbound_token"
)

// Inbound outcomes. A token outcome names its grant, so a refresh is never
// counted as a sign-in. Accepted means the server attempted the redirect to
// sign-in, not that it was delivered or that sign-in finished; an issued
// outcome means the token response was written, not that a client used it.
const (
	outcomeAccepted        attemptOutcome = "accepted"
	outcomeCodeIssued      attemptOutcome = "code_issued"
	outcomeRefreshed       attemptOutcome = "refreshed"
	outcomeRefreshReplayed attemptOutcome = "refresh_replayed"
	outcomeAssertionIssued attemptOutcome = "assertion_issued"
	outcomePolicyBlocked   attemptOutcome = "policy_blocked"
	outcomeUnknown         attemptOutcome = "unknown"
)

const (
	reasonUnobservedExit              attemptReason = "unobserved_exit"
	reasonResponseWriteFailed         attemptReason = "response_write_failed"
	reasonUnavailable                 attemptReason = "unavailable"
	reasonUnexpected                  attemptReason = "unexpected"
	reasonOAuthError                  attemptReason = "oauth_error"
	reasonCIMDFetchFailed             attemptReason = "cimd_fetch_failed"
	reasonUnknownClientID             attemptReason = "unknown_client_id"
	reasonRedirectURINotRegistered    attemptReason = "redirect_uri_not_registered"
	reasonClientBlocked               attemptReason = "ai_tool_blocked"
	reasonFederationUnavailable       attemptReason = "federation_unavailable"
	reasonEndpointNotAgentAddressable attemptReason = "endpoint_not_agent_addressable"
	reasonResourceNotAgentAddressable attemptReason = "resource_not_agent_addressable"
)

// inboundUnverifiedClient is the client key of a request whose client was
// never validated, so no registered client is named.
const inboundUnverifiedClient = "unverified_client"

// policyReasons are refusals an administrator's policy decided: client
// metadata admission, Shadow AI blocking, and workload or agent admission.
// They are blocked, not failed.
var policyReasons = map[string]bool{
	"cimd_admission_denied":    true,
	"cimd_admission_disabled":  true,
	"cimd_denied_disabled":     true,
	"cimd_denied_not_listed":   true,
	"cimd_denied_oversized":    true,
	"cimd_denied_unknown_mode": true,
	"ai_tool_blocked":          true,
	"agent_admission_denied":   true,
	"issuer_untrusted":         true,
	"subject_not_admitted":     true,
}

// notedReasons are the refusal reasons the token handlers log, kept when one
// was logged for the request. Anything else falls back to the response's OAuth
// error code, so a dynamic value never becomes a dimension.
var notedReasons = map[string]bool{
	"agent_admission_denied":                 true,
	"agent_handoff_missing":                  true,
	"agent_target_mismatch":                  true,
	"ai_tool_blocked":                        true,
	"assertion_client_presented_secret":      true,
	"assertion_key_source_missing":           true,
	"assertion_verifier_unavailable":         true,
	"authorization_expired":                  true,
	"cimd_admission_denied":                  true,
	"cimd_admission_disabled":                true,
	"cimd_metadata_invalid":                  true,
	"client_id_names_endpoint":               true,
	"client_method_unreadable":               true,
	"client_secret_mismatch":                 true,
	"code_client_mismatch":                   true,
	"code_endpoint_mismatch":                 true,
	"code_not_found_or_expired":              true,
	"invalid_client":                         true,
	"invalid_grant":                          true,
	"invalid_request":                        true,
	"invalid_scope":                          true,
	"invalid_target":                         true,
	"issuer_changed_during_rotation":         true,
	"missing_client_id":                      true,
	"missing_token":                          true,
	"pkce_mismatch":                          true,
	"public_client_presented_credentials":    true,
	"redirect_uri_mismatch":                  true,
	"refresh_token_client_mismatch":          true,
	"refresh_token_expired":                  true,
	"refresh_token_replay_cache_error":       true,
	"refresh_token_replay_context_cancelled": true,
	"refresh_token_replay_decrypt_error":     true,
	"refresh_token_replay_key_mismatch":      true,
	"refresh_token_replay_payload_invalid":   true,
	"refresh_token_replay_resign_error":      true,
	"refresh_token_replay_session_mismatch":  true,
	"refresh_token_replay_wait_timeout":      true,
	"refresh_token_replay_write_error":       true,
	"refresh_token_resource_mismatch":        true,
	"refresh_token_revoked":                  true,
	"refresh_token_unknown_or_already_used":  true,
	"resource_mismatch":                      true,
	"secret_client_presented_assertion":      true,
	"sign_in_changed_during_rotation":        true,
	"sign_in_revoked":                        true,
	"subject_not_authorized":                 true,
	"temporarily_unavailable":                true,
	"tool_selection_malformed":               true,
	"tool_selection_resource_mismatch":       true,
	"unauthorized_client":                    true,
	"unknown_client_id":                      true,
	"unsupported_grant_type":                 true,
	"upstream_reconnect_required":            true,
}

// workloadReasons are the stage codes a workload assertion refusal carries.
// A code outside them is recorded by its outcome alone.
var workloadReasons = map[string]bool{
	"agent_admission_denied":         true,
	"agent_admission_unavailable":    true,
	"assertion_audience_not_single":  true,
	"assertion_subject_invalid":      true,
	"device_key_id_mismatch":         true,
	"device_key_lookup_rate_limited": true,
	"device_key_not_registered":      true,
	"device_key_unavailable":         true,
	"endpoint_not_agent_addressable": true,
	"issuer_jwks_uri_invalid":        true,
	"issuer_key_source_unknown":      true,
	"issuer_keys_rate_limited":       true,
	"issuer_lookup_rate_limited":     true,
	"issuer_lookup_unavailable":      true,
	"issuer_untrusted":               true,
	"session_persist_unavailable":    true,
	"subject_admission_unavailable":  true,
	"subject_not_admitted":           true,
}

// refuseWorkload records a workload assertion refusal by its stage code:
// admission policy is blocked, an unavailable or rate-limited stage is
// internal, and anything else is the assertion's configuration.
func (o *inboundObservation) refuseWorkload(refusal *workloadGrantError) {
	if o == nil {
		return
	}
	class := classConfiguration
	fallback := attemptReason("workload_assertion_refused")
	if refusal.outcome != workloadGrantRefused {
		class = classInternal
		fallback = "workload_grant_unavailable"
	}
	reason := attemptReason(refusal.reason)
	if !workloadReasons[string(reason)] {
		reason = fallback
	}
	o.refuse(class, reason)
}

// cimdDenialReason names a client metadata admission denial by its typed
// reason; a reason added later is still a denial.
func cimdDenialReason(reason admission.DenialReason) attemptReason {
	named := attemptReason("cimd_" + string(reason))
	if policyReasons[string(named)] {
		return named
	}
	return "cimd_admission_denied"
}

// inboundResult is an inbound attempt's outcome within its stage.
type inboundResult struct {
	// outcome is the attempt's outcome.
	outcome attemptOutcome
	// class is who a failure belongs to; empty for success.
	class failureClass
	// reason is the bounded reason code; empty for success.
	reason attemptReason
}

// inboundObservation is one inbound authorization or token request on a
// configured gateway. Handlers annotate it as the request is served, and it is
// recorded once when the handler returns.
type inboundObservation struct {
	// endpoint is the exact gateway endpoint serving the request.
	endpoint *ResolvedMcpEndpoint
	// stage is inbound_authorize or inbound_token.
	stage string
	// started is when the request reached the handler.
	started time.Time
	// grantType is the token request's grant_type, set once the endpoint
	// accepts it.
	grantType string
	// clientID is the registered client, set only once it is validated.
	clientID uuid.UUID
	// subject is the canonical subject, set only once it is validated.
	subject urn.SessionSubject
	// issued is a success outcome awaiting its written response.
	issued attemptOutcome
	// written reports that the success response was written.
	written bool
	// writeFailed reports that writing the success response failed.
	writeFailed bool
	// explicit is a refusal annotated where its cause is known.
	explicit *inboundResult
	// answered is the refusal a response writer sent, when nothing more
	// specific was annotated.
	answered *inboundResult
	// noted is the bounded reason the handler logged for a refusal.
	noted attemptReason
}

type inboundObservationKey struct{}

// observeInbound starts observing an authorization or token request served by
// a configured gateway. Other endpoints, and the organization gateway, which
// spans projects, are not observed.
func observeInbound(r *http.Request, endpoint *ResolvedMcpEndpoint, stage string) (*http.Request, *inboundObservation) {
	if endpoint == nil || !endpoint.MetaMcpServerID.Valid || endpoint.IsOrganizationGateway() || endpoint.ProjectID == uuid.Nil || endpoint.OrganizationID == "" {
		return r, nil
	}
	obs := &inboundObservation{
		endpoint:    endpoint,
		stage:       stage,
		started:     time.Now(),
		grantType:   "",
		clientID:    uuid.Nil,
		subject:     urn.SessionSubject{Kind: "", ID: ""},
		issued:      "",
		written:     false,
		writeFailed: false,
		explicit:    nil,
		answered:    nil,
		noted:       "",
	}
	return r.WithContext(context.WithValue(r.Context(), inboundObservationKey{}, obs)), obs
}

// inboundObservationFrom is the request's observation, or nil when the
// request is not observed. Every annotation is a no-op on nil.
func inboundObservationFrom(ctx context.Context) *inboundObservation {
	obs, _ := ctx.Value(inboundObservationKey{}).(*inboundObservation)
	return obs
}

func (o *inboundObservation) setGrantType(grantType string) {
	if o != nil {
		o.grantType = grantType
	}
}

// setClient names the registered client once it is validated.
func (o *inboundObservation) setClient(clientID uuid.UUID) {
	if o != nil {
		o.clientID = clientID
	}
}

// setSubject names the canonical caller once a handler has validated who it
// is, so a later refusal is attributed to them. Before that it stays empty.
func (o *inboundObservation) setSubject(subject urn.SessionSubject) {
	if o != nil {
		o.subject = subject
	}
}

// issue marks the success the handler is about to write, for the canonical
// subject it minted. It counts only once the response is written.
func (o *inboundObservation) issue(outcome attemptOutcome, subject urn.SessionSubject) {
	if o != nil {
		o.issued = outcome
		o.subject = subject
	}
}

func (o *inboundObservation) accept() {
	if o != nil {
		o.issued = outcomeAccepted
		o.written = true
	}
}

func (o *inboundObservation) responseWritten(err error) {
	if o == nil {
		return
	}
	if err != nil {
		o.writeFailed = true
		return
	}
	o.written = true
}

// refuse records a refusal whose cause is known where it happens.
func (o *inboundObservation) refuse(class failureClass, reason attemptReason) {
	if o == nil || o.explicit != nil {
		return
	}
	outcome := outcomeFailed
	if policyReasons[string(reason)] {
		outcome = outcomePolicyBlocked
	}
	o.explicit = &inboundResult{outcome: outcome, class: class, reason: reason}
}

// note keeps a refusal reason the handler logged, when it is one of the
// known codes. It only refines a refusal a writer sends; it never makes one.
func (o *inboundObservation) note(reason string) {
	if o == nil || !notedReasons[reason] {
		return
	}
	o.noted = attemptReason(reason)
}

// answer records the OAuth error a response writer sent. Server-side
// statuses are internal; the rest are the request's configuration.
func (o *inboundObservation) answer(status int, code string) {
	if o == nil || o.answered != nil {
		return
	}
	class := classConfiguration
	if status >= http.StatusInternalServerError || status == http.StatusTooManyRequests {
		class = classInternal
	}
	o.answered = &inboundResult{outcome: outcomeFailed, class: class, reason: oauthReason(code)}
}

// result is the attempt's outcome once the handler has returned err.
func (o *inboundObservation) result(ctx context.Context, err error) inboundResult {
	switch {
	case o.issued != "" && o.written:
		return inboundResult{outcome: o.issued, class: classNone, reason: reasonNone}
	case o.issued != "" && o.writeFailed:
		if ctx.Err() != nil {
			return inboundResult{outcome: outcomeFailed, class: classCanceled, reason: reasonCanceled}
		}
		return inboundResult{outcome: outcomeFailed, class: classInternal, reason: reasonResponseWriteFailed}
	case o.explicit != nil:
		return *o.explicit
	case o.answered != nil:
		answered := *o.answered
		if o.noted != "" {
			answered.reason = o.noted
			if policyReasons[string(o.noted)] {
				answered.outcome = outcomePolicyBlocked
			}
		}
		return answered
	case err != nil:
		return inboundErrorResult(ctx, err)
	}
	// The handler returned without a recorded outcome: kept as unknown so the
	// attempt still counts, never as a success.
	return inboundResult{outcome: outcomeUnknown, class: classInternal, reason: reasonUnobservedExit}
}

// inboundErrorResult classifies an error the handler returned for the outer
// error handler to write.
func inboundErrorResult(ctx context.Context, err error) inboundResult {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return inboundResult{outcome: outcomeFailed, class: classCanceled, reason: reasonCanceled}
	}
	if oauthErr, ok := errors.AsType[*oops.OAuthError](err); ok {
		class := classConfiguration
		if oauthErr.Status >= http.StatusInternalServerError {
			class = classInternal
		}
		return inboundResult{outcome: outcomeFailed, class: class, reason: oauthReason(oauthErr.Response.Code)}
	}
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		return shareableResult(shareable.Code)
	}
	return inboundResult{outcome: outcomeFailed, class: classInternal, reason: reasonUnexpected}
}

// shareableResult classifies an error by its code alone: a refusal of the
// request is its configuration, a dependency outage is internal, and any other
// code is an unexpected internal fault.
func shareableResult(code oops.Code) inboundResult {
	failed := func(class failureClass, reason attemptReason) inboundResult {
		return inboundResult{outcome: outcomeFailed, class: class, reason: reason}
	}
	switch code {
	case oops.CodeCanceled:
		return failed(classCanceled, reasonCanceled)
	case oops.CodeUnavailable, oops.CodeGatewayError, oops.CodeRateLimitExceeded:
		return failed(classInternal, reasonUnavailable)
	case oops.CodeBadRequest, oops.CodeParseError, oops.CodeInvalid, oops.CodeRequestTooLarge,
		oops.CodeUnsupportedMedia, oops.CodeMethodNotAllowed:
		return failed(classConfiguration, "invalid_request")
	case oops.CodeNotFound:
		return failed(classConfiguration, "not_found")
	case oops.CodeUnauthorized:
		return failed(classConfiguration, "unauthorized")
	case oops.CodeForbidden:
		return failed(classConfiguration, "forbidden")
	case oops.CodeConflict, oops.CodeFailedPrecondition:
		return failed(classConfiguration, "conflict")
	case oops.CodeUnexpected, oops.CodeInvariantViolation, oops.CodeNotImplemented,
		oops.CodeInsufficientCredits, oops.CodeInferenceDisabled:
		return failed(classInternal, reasonUnexpected)
	}
	return failed(classInternal, reasonUnexpected)
}

// oauthReason is an OAuth error code as a reason; anything outside the
// codes the endpoints send is one bounded value.
func oauthReason(code string) attemptReason {
	switch code {
	case "invalid_request", "invalid_client", "invalid_grant", "unauthorized_client",
		"unsupported_grant_type", "invalid_scope", "invalid_target", "access_denied",
		"unsupported_response_type", "temporarily_unavailable", "server_error":
		return attemptReason(code)
	}
	return reasonOAuthError
}

// finishInbound records the observed request once, independent of its
// response: the write runs after the handler and never changes what was sent.
func (s *Service) finishInbound(ctx context.Context, obs *inboundObservation, err error) {
	if obs == nil {
		return
	}
	result := obs.result(ctx, err)
	caller := attemptCaller{
		projectID:           obs.endpoint.ProjectID,
		organizationID:      obs.endpoint.OrganizationID,
		gatewayID:           obs.endpoint.MetaMcpServerID.UUID,
		userSessionIssuerID: obs.endpoint.UserSessionIssuerID,
		subject:             obs.subject,
		oauthClientID:       "",
		traceID:             traceIDOf(ctx),
	}
	clientKey := inboundUnverifiedClient
	if obs.clientID != uuid.Nil {
		clientKey = obs.clientID.String()
	}
	go s.writeInboundAttempt(context.WithoutCancel(ctx), caller, obs.started, obs.stage, obs.grantType, result, clientKey)
}

func (s *Service) writeInboundAttempt(ctx context.Context, caller attemptCaller, observedAt time.Time, stage, grantType string, result inboundResult, clientKey string) {
	ctx, cancel := context.WithTimeout(ctx, connectionAttemptWriteTimeout)
	defer cancel()
	id, err := uuid.NewV7()
	if err != nil {
		s.logger.WarnContext(ctx, "mint inbound attempt id", attr.SlogError(err))
		return
	}
	attrs := caller.attemptAttributes(id, stage, result.outcome, result.class, result.reason, clientKey)
	if grantType != "" {
		attrs[attr.ConnectionAttemptGrantTypeKey] = grantType
	}
	if err := s.telemLogger.LogBulkBounded(ctx, []tm.LogParams{caller.attemptRow(observedAt, attrs)}); err != nil {
		s.logger.WarnContext(ctx, "record inbound attempt", attr.SlogError(err), attr.SlogMetaMcpServerID(caller.gatewayID.String()))
	}
}
