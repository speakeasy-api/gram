package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	remotemcp_repo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	tunneledmcprepo "github.com/speakeasy-api/gram/server/internal/tunneledmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// MCPConnectionAttemptEventURN identifies connection attempt telemetry rows.
// The mcp_connection_attempt_summaries materialized view filters on it.
var MCPConnectionAttemptEventURN = urn.NewTelemetryEvent(urn.TelemetryEventOriginGramService, urn.TelemetryEventKindLog, "mcp_connection_attempt").String()

// connectionAttemptWriteTimeout bounds recording one listing's attempts,
// client lookup included. The write runs independently of the response, so it
// never holds the listing, and a canceled request is still recorded.
const connectionAttemptWriteTimeout = 2 * time.Second

// attemptClientLookupTimeout is the part of the write's time the optional
// client lookup may use, so a slow lookup still leaves time to record.
const attemptClientLookupTimeout = 500 * time.Millisecond

// unapprovedMemberCheckTimeout bounds the local checks of every member added
// since consent, so they never hold a listing for long.
const unapprovedMemberCheckTimeout = 500 * time.Millisecond

const (
	attemptStageListing = "discovery_listing"
	attemptStageMember  = "discovery_member"
)

// attemptOutcome is an attempt's result within its stage.
type attemptOutcome string

const (
	outcomeComplete          attemptOutcome = "complete"
	outcomePartial           attemptOutcome = "partial"
	outcomeFailed            attemptOutcome = "failed"
	outcomeListed            attemptOutcome = "listed"
	outcomeCredentialBlocked attemptOutcome = "credential_blocked" //nolint:gosec // G101 false positive: a reason label.
	outcomeConsentBlocked    attemptOutcome = "consent_blocked"
)

// failureClass names who a failed attempt belongs to.
type failureClass string

const (
	classNone          failureClass = ""
	classEndpoint      failureClass = "endpoint"
	classConfiguration failureClass = "configuration"
	classInternal      failureClass = "internal"
	classCanceled      failureClass = "canceled"
)

// attemptReason is a sanitized reason code. It is set where a member failure
// is built, never read from a message.
type attemptReason string

const (
	reasonNone attemptReason = ""

	// Member failures the member's endpoint or its authorization server owns.
	reasonRateLimited                        attemptReason = "rate_limited"
	reasonUpstreamUnavailable                attemptReason = "upstream_unavailable"
	reasonUpstreamRejected                   attemptReason = "upstream_rejected"
	reasonListingTimeout                     attemptReason = "listing_timeout"
	reasonResponseTooLarge                   attemptReason = "response_too_large"
	reasonUnreachable                        attemptReason = "unreachable"
	reasonInvalidListing                     attemptReason = "invalid_listing"
	reasonInvalidResponse                    attemptReason = "invalid_response"
	reasonIncompleteCatalog                  attemptReason = "incomplete_catalog"
	reasonAuthorizationServerUnavailable     attemptReason = "authorization_server_unavailable"
	reasonEnterpriseAuthorizationUnavailable attemptReason = "enterprise_authorization_unavailable"

	// Member failures the gateway owns.
	reasonListingBudgetExhausted attemptReason = "listing_budget_exhausted"
	reasonCredentialLoadFailed   attemptReason = "credential_load_failed" //nolint:gosec // G101 false positive: a reason label.
	reasonConnectionCheckFailed  attemptReason = "connection_check_failed"
	reasonUnclassified           attemptReason = "unclassified"

	// Member configuration. A member that is not servable is excluded from
	// listings unrecorded, as before attempts were recorded; its reason only
	// names the error.
	reasonNotServable                   attemptReason = "not_servable"
	reasonNeedsSignIn                   attemptReason = "needs_sign_in"
	reasonCredentialRejected            attemptReason = "credential_rejected"      //nolint:gosec // G101 false positive: a reason label.
	reasonCredentialMisconfigured       attemptReason = "credential_misconfigured" //nolint:gosec // G101 false positive: a reason label.
	reasonCredentialAmbiguous           attemptReason = "credential_ambiguous"     //nolint:gosec // G101 false positive: a reason label.
	reasonReauthenticationRequired      attemptReason = "reauthentication_required"
	reasonEnterpriseAuthorizationFailed attemptReason = "enterprise_authorization_failed"
	reasonConsentNotApproved            attemptReason = "consent_not_approved"
	reasonConsentAuthChanged            attemptReason = "consent_auth_changed"
	reasonConsentConfigChanged          attemptReason = "consent_config_changed"

	// Listing failures.
	reasonInvalidRequest       attemptReason = "invalid_request"
	reasonStaleCursor          attemptReason = "stale_cursor"
	reasonMemberSnapshotFailed attemptReason = "member_snapshot_failed"
	reasonAmbiguousToolName    attemptReason = "ambiguous_tool_name"
	reasonInventoryTooLarge    attemptReason = "inventory_too_large"
	reasonInventoryError       attemptReason = "inventory_error"
	reasonEncodingFailed       attemptReason = "encoding_failed"
	reasonCanceled             attemptReason = "canceled"
	reasonNoMemberListed       attemptReason = "no_member_listed"
	reasonMemberCheckFailed    attemptReason = "member_check_failed"
)

// memberReasonClass is what a member reason means: its outcome, its failure
// class, and the fixed text a left-out member's row carries.
type memberReasonClass struct {
	outcome  attemptOutcome
	class    failureClass
	omission string
}

// memberReasonMeaning is what a member reason means; false for a reason that
// is not a member result.
func memberReasonMeaning(reason attemptReason) (memberReasonClass, bool) {
	failed := func(class failureClass, omission string) (memberReasonClass, bool) {
		return memberReasonClass{outcome: outcomeFailed, class: class, omission: omission}, true
	}
	blocked := func(outcome attemptOutcome) (memberReasonClass, bool) {
		return memberReasonClass{outcome: outcome, class: classConfiguration, omission: ""}, true
	}
	switch reason {
	case reasonRateLimited:
		return failed(classEndpoint, "was rate limited")
	case reasonUpstreamUnavailable:
		return failed(classEndpoint, "was unavailable")
	case reasonUpstreamRejected:
		return failed(classEndpoint, "rejected the tool listing")
	case reasonListingTimeout:
		return failed(classEndpoint, "did not answer in time")
	case reasonResponseTooLarge:
		return failed(classEndpoint, "returned a tool listing that was too large")
	case reasonUnreachable:
		return failed(classEndpoint, "was unreachable")
	case reasonInvalidListing, reasonIncompleteCatalog:
		return failed(classEndpoint, "returned tool definitions the gateway could not list")
	case reasonInvalidResponse:
		return failed(classEndpoint, "returned a response the gateway could not read")
	case reasonAuthorizationServerUnavailable:
		return failed(classEndpoint, "could not reach its authorization server")
	case reasonEnterpriseAuthorizationUnavailable:
		return failed(classEndpoint, "could not reach its enterprise authorization")
	case reasonListingBudgetExhausted:
		return failed(classInternal, "was not reached within the gateway listing time")
	case reasonCredentialLoadFailed, reasonConnectionCheckFailed, reasonUnclassified:
		return failed(classInternal, "could not be listed")
	case reasonNeedsSignIn, reasonCredentialRejected, reasonCredentialMisconfigured, reasonCredentialAmbiguous, reasonReauthenticationRequired, reasonEnterpriseAuthorizationFailed:
		return blocked(outcomeCredentialBlocked)
	case reasonConsentNotApproved, reasonConsentAuthChanged, reasonConsentConfigChanged:
		return blocked(outcomeConsentBlocked)
	default:
		return memberReasonClass{outcome: "", class: classNone, omission: ""}, false
	}
}

// memberAttempt is one caller-visible member's final result in a listing.
type memberAttempt struct {
	member  metaMember
	outcome attemptOutcome
	class   failureClass
	reason  attemptReason

	// calls is how often the listing described the member; 0 when the
	// listing ran out of time before the member got a slot.
	calls int

	// status is the upstream HTTP status of a failed member, or 0 when the
	// upstream did not answer.
	status int
}

// listingAttempt is one Direct tools/list call's result.
type listingAttempt struct {
	outcome attemptOutcome
	class   failureClass
	reason  attemptReason
}

// listingFromMembers stands for a listing that answered: its result follows
// from its members once the writer has checked those added since consent.
var listingFromMembers = listingAttempt{outcome: "", class: classNone, reason: reasonNone}

// memberAttemptFor classifies a member's describe result. Failed members are
// those the listing left its tools unknown for: a discovery error or an
// incomplete catalog. Blocked members are those whose credential or consent
// the caller fixes. Everything else stays unrecorded, as before: members the
// caller may not describe, and members that are not servable.
func memberAttemptFor(member metaMember, result memberDescribeResult) (memberAttempt, bool) {
	attempt := memberAttempt{member: member, outcome: outcomeListed, class: classNone, reason: reasonNone, calls: result.calls, status: 0}
	if result.err == nil {
		if result.catalog != nil && !result.catalog.incomplete {
			return attempt, true
		}
		attempt.outcome, attempt.class, attempt.reason = outcomeFailed, classEndpoint, reasonIncompleteCatalog
		return attempt, true
	}
	if memberAccessDenied(result.err) {
		return attempt, false
	}
	if discovery, ok := errors.AsType[memberDiscoveryError](result.err); ok {
		attempt.reason, attempt.status = discovery.reason, discovery.status
		meaning, known := memberReasonMeaning(attempt.reason)
		if !known || meaning.outcome != outcomeFailed {
			attempt.reason = reasonUnclassified
			meaning, _ = memberReasonMeaning(reasonUnclassified)
		}
		attempt.outcome, attempt.class = meaning.outcome, meaning.class
		return attempt, true
	}
	failure, ok := errors.AsType[*metaMemberError](result.err)
	if !ok {
		return attempt, false
	}
	meaning, known := memberReasonMeaning(failure.reason)
	if !known || (meaning.outcome != outcomeCredentialBlocked && meaning.outcome != outcomeConsentBlocked) {
		return attempt, false
	}
	attempt.outcome, attempt.class, attempt.reason = meaning.outcome, meaning.class, failure.reason
	return attempt, true
}

// unapprovedMemberAttempt records a visible member the connection's consent
// did not approve, which a listing never describes.
func unapprovedMemberAttempt(member metaMember) memberAttempt {
	return memberAttempt{member: member, outcome: outcomeConsentBlocked, class: classConfiguration, reason: reasonConsentNotApproved, calls: 0, status: 0}
}

// unapprovedMemberAttempts records the members added since consent that the
// caller could use after approving them. A listing never describes them, so
// each first passes the local checks a described member does: its backend
// still exists and the caller may connect to it. No upstream is contacted and
// no credential is used. A member that fails them stays unrecorded, as it
// would if approved. A member whose check could not finish is not recorded
// either, and unverified reports that the listing's result is uncertain.
func (s *Service) unapprovedMemberAttempts(ctx context.Context, logger *slog.Logger, gate *metaGateContext, unapproved []metaMember) (attempts []memberAttempt, unverified bool) {
	if len(unapproved) == 0 {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, unapprovedMemberCheckTimeout)
	defer cancel()
	attempts = make([]memberAttempt, 0, len(unapproved))
	for _, member := range unapproved {
		if ctx.Err() != nil {
			logger.WarnContext(ctx, "checking unapproved gateway members ran out of time")
			return attempts, true
		}
		err := s.memberAvailable(ctx, logger, gate, member)
		failure, typed := errors.AsType[*metaMemberError](err)
		switch {
		case err == nil:
			attempts = append(attempts, unapprovedMemberAttempt(member))
		case memberAccessDenied(err), typed && failure.reason == reasonNotServable:
		default:
			unverified = true
			logger.WarnContext(ctx, "check unapproved gateway member", attr.SlogMcpServerID(member.serverID.String()), attr.SlogError(err))
		}
	}
	return attempts, unverified
}

// uncertainListing marks a listing that left a member unchecked: it cannot be
// complete, since that member's result is unknown. A failed listing keeps its
// own reason.
func uncertainListing(listing listingAttempt) listingAttempt {
	if listing.outcome == outcomeFailed {
		return listing
	}
	return listingAttempt{outcome: outcomePartial, class: classInternal, reason: reasonMemberCheckFailed}
}

// memberAvailable reports why a member could not be served to this caller,
// from local state alone: a hosted member's toolset and its connect gate, or
// a proxied member's backend row.
func (s *Service) memberAvailable(ctx context.Context, logger *slog.Logger, gate *metaGateContext, member metaMember) error {
	notServable := &metaMemberError{message: fmt.Sprintf("server %q is not currently servable", member.slug), reason: reasonNotServable}
	var err error
	switch {
	case member.backend == metaMemberBackendHosted:
		_, _, err = s.loadMemberToolset(ctx, logger, gate, member)
		return err
	case member.remoteServerID.Valid:
		_, err = remotemcp_repo.New(s.db).GetServerByID(ctx, remotemcp_repo.GetServerByIDParams{ID: member.remoteServerID.UUID, ProjectID: member.projectID})
	case member.tunneledServerID.Valid:
		_, err = tunneledmcprepo.New(s.db).GetServerByID(ctx, tunneledmcprepo.GetServerByIDParams{ID: member.tunneledServerID.UUID, ProjectID: member.projectID})
	default:
		return notServable
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return notServable
	}
	if err != nil {
		return fmt.Errorf("load gateway member backend: %w", err)
	}
	return nil
}

// listingResult summarizes a listing that answered: complete when every
// recorded member listed, partial when some did, and failed when members were
// recorded and none listed. A failed listing takes the most actionable class
// among its members: the endpoint, then the gateway, then configuration.
func listingResult(attempts []memberAttempt) listingAttempt {
	listed, class := 0, classNone
	for _, attempt := range attempts {
		if attempt.outcome == outcomeListed {
			listed++
			continue
		}
		if class == classNone || failureClassRank(attempt.class) < failureClassRank(class) {
			class = attempt.class
		}
	}
	switch {
	case listed == len(attempts):
		return listingAttempt{outcome: outcomeComplete, class: classNone, reason: reasonNone}
	case listed > 0:
		return listingAttempt{outcome: outcomePartial, class: classNone, reason: reasonNone}
	}
	return listingAttempt{outcome: outcomeFailed, class: class, reason: reasonNoMemberListed}
}

func failureClassRank(class failureClass) int {
	switch class {
	case classEndpoint:
		return 0
	case classInternal:
		return 1
	case classConfiguration:
		return 2
	case classNone, classCanceled:
		return 3
	}
	return 3
}

// listingFailure is a listing that returned an error. A canceled request is
// the caller's, whatever failed first.
func listingFailure(ctx context.Context, class failureClass, reason attemptReason) listingAttempt {
	if ctx.Err() != nil {
		return listingAttempt{outcome: outcomeFailed, class: classCanceled, reason: reasonCanceled}
	}
	return listingAttempt{outcome: outcomeFailed, class: class, reason: reason}
}

// inventoryFailure classifies an error that stopped collecting the inventory.
func inventoryFailure(ctx context.Context, err error) listingAttempt {
	shareable, _ := errors.AsType[*oops.ShareableError](err)
	switch {
	case shareable != nil && shareable.Code == oops.CodeConflict:
		return listingFailure(ctx, classConfiguration, reasonAmbiguousToolName)
	case shareable != nil && shareable.Code == oops.CodeRequestTooLarge:
		return listingFailure(ctx, classConfiguration, reasonInventoryTooLarge)
	}
	return listingFailure(ctx, classInternal, reasonInventoryError)
}

// encodingFailure classifies an error encoding a collected inventory.
func encodingFailure(ctx context.Context, err error) listingAttempt {
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok && shareable.Code == oops.CodeRequestTooLarge {
		return listingFailure(ctx, classConfiguration, reasonInventoryTooLarge)
	}
	return listingFailure(ctx, classInternal, reasonEncodingFailed)
}

// attemptCaller is who made a listing, captured on the request path so the
// write can run after it.
type attemptCaller struct {
	projectID           uuid.UUID
	organizationID      string
	gatewayID           uuid.UUID
	userSessionIssuerID uuid.UUID
	subject             urn.SessionSubject

	// oauthClientID is the client the bearer authenticated as. It may be a
	// CIMD URL, so it is only ever a lookup key, never recorded.
	oauthClientID string

	traceID string
}

// recordDirectListing writes a Direct tools/list call's listing row and one
// row per recorded member. members is nil when the listing failed before its
// members were known, and then only the listing is written. Best effort and
// independent of the response: failures are logged and never fail the
// listing, and checking members added since consent never delays it.
func (s *Service) recordDirectListing(ctx context.Context, gate *metaGateContext, observedAt time.Time, listing listingAttempt, members []memberAttempt) {
	caller, ok := attemptCallerFor(ctx, gate)
	if !ok {
		return
	}
	check := *gate
	var unapproved []metaMember
	if members != nil {
		unapproved = slices.Clone(gate.unapprovedMembers)
	}
	go s.writeDirectListing(context.WithoutCancel(ctx), caller, &check, observedAt, listing, members, unapproved)
}

// attemptCallerFor captures who made a listing, or reports false when the
// listing has no project, gateway, or organization to record it under. An
// organization gateway spans projects, so its listings are not recorded.
func attemptCallerFor(ctx context.Context, gate *metaGateContext) (attemptCaller, bool) {
	var none attemptCaller
	if gate.projectID == uuid.Nil || gate.metaServerID == uuid.Nil || gate.organizationID == "" {
		return none, false
	}
	oauthClientID, _ := contextvalues.GetOAuthClientID(ctx)
	traceID := ""
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.HasTraceID() {
		traceID = spanCtx.TraceID().String()
	}
	return attemptCaller{
		projectID:           gate.projectID,
		organizationID:      gate.organizationID,
		gatewayID:           gate.metaServerID,
		userSessionIssuerID: gate.userSessionIssuerID,
		subject:             gate.subject,
		oauthClientID:       oauthClientID,
		traceID:             traceID,
	}, true
}

func (s *Service) writeDirectListing(ctx context.Context, caller attemptCaller, gate *metaGateContext, observedAt time.Time, listing listingAttempt, members []memberAttempt, unapproved []metaMember) {
	ctx, cancel := context.WithTimeout(ctx, connectionAttemptWriteTimeout)
	defer cancel()
	if members != nil {
		added, unverified := s.unapprovedMemberAttempts(ctx, s.logger, gate, unapproved)
		members = append(members, added...)
		if listing == listingFromMembers {
			listing = listingResult(members)
			if unverified {
				listing = uncertainListing(listing)
			}
		}
	}
	clientKey := s.attemptClientKey(ctx, caller)
	listingID, err := uuid.NewV7()
	if err != nil {
		s.logger.WarnContext(ctx, "mint gateway listing attempt id", attr.SlogError(err))
		return
	}
	rows := make([]tm.LogParams, 0, 1+len(members))
	listingAttrs := caller.attemptAttributes(listingID, attemptStageListing, listing.outcome, listing.class, listing.reason, clientKey)
	rows = append(rows, caller.attemptRow(observedAt, listingAttrs))
	for _, member := range members {
		id, err := uuid.NewV7()
		if err != nil {
			s.logger.WarnContext(ctx, "mint gateway member attempt id", attr.SlogError(err))
			return
		}
		attrs := caller.attemptAttributes(id, attemptStageMember, member.outcome, member.class, member.reason, clientKey)
		attrs[attr.ConnectionAttemptListingIDKey] = listingID.String()
		attrs[attr.McpServerIDKey] = member.member.serverID.String()
		attrs[attr.ConnectionAttemptCallsKey] = member.calls
		attrs[attr.ConnectionAttemptRetriesKey] = max(member.calls-1, 0)
		if member.member.remoteSessionIssuerID.Valid {
			attrs[attr.ConnectionAttemptProviderIDKey] = member.member.remoteSessionIssuerID.UUID.String()
		}
		row := caller.attemptRow(observedAt, attrs)
		if member.outcome == outcomeFailed {
			caller.labelOmission(&row, member)
		}
		rows = append(rows, row)
	}
	if err := s.telemLogger.LogBulkBounded(ctx, rows); err != nil {
		s.logger.WarnContext(ctx, "record gateway listing attempts", attr.SlogError(err), attr.SlogMetaMcpServerID(caller.gatewayID.String()))
	}
}

// attemptClientKey is the registered client the caller authenticated as, or
// a bounded kind when there is none to name.
func (s *Service) attemptClientKey(ctx context.Context, caller attemptCaller) string {
	if caller.oauthClientID != "" && caller.userSessionIssuerID != uuid.Nil {
		ctx, cancel := context.WithTimeout(ctx, attemptClientLookupTimeout)
		defer cancel()
		client, err := usersessionsrepo.New(s.db).GetUserSessionClientByClientID(ctx, usersessionsrepo.GetUserSessionClientByClientIDParams{
			UserSessionIssuerID: caller.userSessionIssuerID,
			ClientID:            caller.oauthClientID,
		})
		switch {
		case err == nil:
			return client.ID.String()
		case errors.Is(err, pgx.ErrNoRows):
			return "unregistered_client"
		}
		s.logger.WarnContext(ctx, "resolve gateway listing client", attr.SlogError(err))
		return "unknown"
	}
	if caller.subject.IsZero() {
		return "anonymous"
	}
	return "credential:" + string(caller.subject.Kind)
}

func (c attemptCaller) attemptAttributes(id uuid.UUID, stage string, outcome attemptOutcome, class failureClass, reason attemptReason, clientKey string) map[attr.Key]any {
	attrs := map[attr.Key]any{
		attr.EventURNKey:                      MCPConnectionAttemptEventURN,
		attr.MetaMcpServerIDKey:               c.gatewayID.String(),
		attr.ConnectionAttemptIDKey:           id.String(),
		attr.ConnectionAttemptStageKey:        stage,
		attr.ConnectionAttemptOutcomeKey:      string(outcome),
		attr.ConnectionAttemptFailureClassKey: string(class),
		attr.ConnectionAttemptReasonKey:       string(reason),
		attr.ConnectionAttemptClientKeyKey:    clientKey,
	}
	if !c.subject.IsZero() {
		attrs[attr.ConnectionAttemptSubjectKey] = c.subject.String()
	}
	if c.traceID != "" {
		attrs[attr.ConnectionAttemptTraceIDKey] = c.traceID
	}
	return attrs
}

// attemptRow carries no tool identity, trace, span, or chat, so it never
// counts as tool usage, a trace, or a chat request.
func (c attemptCaller) attemptRow(observedAt time.Time, attrs map[attr.Key]any) tm.LogParams {
	return tm.LogParams{
		Timestamp: observedAt,
		ToolInfo: tm.ToolInfo{
			ID:             "",
			URN:            "",
			Name:           "",
			ProjectID:      c.projectID.String(),
			DeploymentID:   "",
			FunctionID:     nil,
			OrganizationID: c.organizationID,
		},
		UserInfo:   tm.UserInfoByID(""),
		Attributes: attrs,
	}
}

// labelOmission gives a left-out member's row the gateway discovery labels the
// Activity drill-in searches by, with fixed text naming the member, so one row
// is both the attempt and the omission. The member's own message can quote its
// upstream's response, so it is never used.
func (c attemptCaller) labelOmission(row *tm.LogParams, member memberAttempt) {
	row.ToolInfo.ID = c.gatewayID.String()
	row.ToolInfo.URN = "metamcp:" + c.gatewayID.String() + ":tools/list"
	row.ToolInfo.Name = "tools/list"
	row.Attributes[attr.EventSourceKey] = string(tm.EventSourceMetaDiscovery)
	status := member.status
	if status == 0 {
		status = http.StatusBadGateway
	}
	tm.HTTPLogAttributes(row.Attributes).RecordStatusCode(status)
	meaning, _ := memberReasonMeaning(member.reason)
	row.Attributes[attr.ErrorMessageKey] = fmt.Sprintf("server %q %s and was left out of the tool listing", member.member.slug, meaning.omission)
}
