package mcp

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Where a shadowed mandatory check ran.
const (
	mandatorySurfaceHostedConnect  = "hosted_connect"
	mandatorySurfaceProxiedConnect = "proxied_connect"
	mandatorySurfaceGatewayMember  = "gateway_member"
	mandatorySurfaceAuthClient     = "auth_client"
	mandatorySurfaceTokenIssuance  = "token_issuance"
	mandatorySurfaceConsent        = "consent"
)

// Caller classes from the RFC's public-server table. A credential that fails
// admission shows as reason inactive_credential.
const (
	mandatoryCallerAnonymous     = "anonymous"
	mandatoryCallerUnrepresented = "unrepresented"
	mandatoryCallerRepresented   = "represented"
	mandatoryCallerCrossOrg      = "cross_org"
)

const (
	mandatoryOutcomeAllow   = "allow"
	mandatoryOutcomeDeny    = "deny"
	mandatoryOutcomeError   = "error"
	mandatoryOutcomeSkipped = "skipped"

	// mandatoryReasonResolverOff is an auth client used without the typed
	// resolver, so no client was evaluated.
	mandatoryReasonResolverOff = "resolver_off"

	mandatoryShadowTimeout = 250 * time.Millisecond

	mandatoryShadowLogTTL  = 10 * time.Minute
	mandatoryShadowLogSize = 4096
)

// mandatoryShadowLog dedupes disagreement logs: the counter has every
// occurrence, the log names each distinct one.
type mandatoryShadowLog struct {
	mu   sync.Mutex
	seen *expirable.LRU[string, struct{}]
}

func newMandatoryShadowLog() *mandatoryShadowLog {
	return &mandatoryShadowLog{mu: sync.Mutex{}, seen: expirable.NewLRU[string, struct{}](mandatoryShadowLogSize, nil, mandatoryShadowLogTTL)}
}

// first reports whether key is new, and remembers it.
func (l *mandatoryShadowLog) first(key string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, seen := l.seen.Get(key); seen {
		return false
	}
	l.seen.Add(key, struct{}{})
	return true
}

// mandatoryShadow evaluates one request's mandatory checks beside today's
// decisions, admitting the caller once. A nil shadow records nothing.
type mandatoryShadow struct {
	service        *Service
	organizationID string
	authorization  authz.MandatoryAuthorization
	err            error
}

// newMandatoryShadow prepares the shadow for a server in organizationID, or
// returns nil when the organization does not shadow.
func (s *Service) newMandatoryShadow(ctx context.Context, organizationID string) *mandatoryShadow {
	if !feature.MandatoryAuthzShadow(ctx, s.features, organizationID) {
		return nil
	}
	prepareCtx, cancel := context.WithTimeout(ctx, mandatoryShadowTimeout)
	defer cancel()
	authorization, err := s.authz.PrepareMandatory(prepareCtx)
	return &mandatoryShadow{service: s, organizationID: organizationID, authorization: authorization, err: err}
}

// shadowMandatory records one check against today's decision.
func (s *Service) shadowMandatory(ctx context.Context, surface, organizationID string, legacyAllowed bool, check authz.Check) {
	s.newMandatoryShadow(ctx, organizationID).record(ctx, surface, legacyAllowed, check)
}

// shadowRequire records check against today's Require result. Only access
// denials count as a deny; other errors fail the request and are not decisions.
func (m *mandatoryShadow) shadowRequire(ctx context.Context, surface string, requireErr error, check authz.Check) {
	if requireErr != nil && !isAccessDenial(requireErr) {
		return
	}
	m.record(ctx, surface, requireErr == nil, check)
}

func isAccessDenial(err error) bool {
	var shareable *oops.ShareableError
	return errors.As(err, &shareable) && (shareable.Code == oops.CodeForbidden || shareable.Code == oops.CodeUnauthorized || shareable.Code == oops.CodeNotFound)
}

// mandatoryAuthClientCheck is the check for using a resolved client. A
// personal client is checked against the connection's stored owner.
func mandatoryAuthClientCheck(projectID, clientID uuid.UUID, clientType string, subject urn.SessionSubject) authz.Check {
	owner := ""
	if clientType == authClientTypeRemoteSession && subject.Kind == urn.SessionSubjectKindUser {
		owner = subject.ID
	}
	return authz.MCPAuthClientCheck(clientID.String(), authz.MCPAuthClientDimensions{ClientType: clientType, ProjectID: projectID.String(), ConnectionOwner: owner})
}

func (m *mandatoryShadow) callerClass() string {
	switch {
	case m.authorization.Caller() == authz.MandatoryCallerAnonymous:
		return mandatoryCallerAnonymous
	case m.authorization.OrganizationID() != m.organizationID:
		return mandatoryCallerCrossOrg
	case m.authorization.RepresentedUserID() == "":
		return mandatoryCallerUnrepresented
	default:
		return mandatoryCallerRepresented
	}
}

// record evaluates check and records whether it agrees with legacyAllowed,
// today's decision. It never changes the request.
func (m *mandatoryShadow) record(ctx context.Context, surface string, legacyAllowed bool, check authz.Check) {
	if m == nil {
		return
	}
	if m.err != nil && ctx.Err() != nil {
		// The client went away; there is no decision to compare.
		return
	}
	var decision authz.MandatoryDecision
	err := m.err
	if err == nil {
		decision, err = m.authorization.Decide(m.organizationID, check)
	}
	m.emit(ctx, surface, legacyAllowed, check.ResourceID, check.Dimensions[authz.SelectorKeyProjectID], decision, err, "")
}

// recordSkipped records a decision today's path made that the mandatory
// check cannot evaluate yet, such as an auth client chosen without the
// typed resolver. A skipped check is never an allow.
func (m *mandatoryShadow) recordSkipped(ctx context.Context, surface, reason, resourceID, projectID string) {
	if m == nil {
		return
	}
	m.emit(ctx, surface, true, resourceID, projectID, authz.MandatoryDecision{Allowed: false, Source: authz.MandatoryPolicySourceNone, Reason: authz.MandatoryDenyReasonNone}, nil, reason)
}

func (m *mandatoryShadow) emit(ctx context.Context, surface string, legacyAllowed bool, resourceID, projectID string, decision authz.MandatoryDecision, err error, skipped string) {
	s := m.service
	outcome, reason := mandatoryOutcomeDeny, string(decision.Reason)
	switch {
	case skipped != "":
		outcome, reason = mandatoryOutcomeSkipped, skipped
	case err != nil:
		outcome = mandatoryOutcomeError
	case decision.Allowed:
		outcome = mandatoryOutcomeAllow
	}
	source := string(decision.Source)
	if source == "" {
		source = "none"
	}
	if reason == "" {
		reason = "none"
	}
	shadow := mcpmetrics.MandatoryAuthzShadow{
		OrganizationID: m.organizationID,
		Surface:        surface,
		Caller:         m.callerClass(),
		Principal:      string(m.authorization.Caller()),
		LegacyAllowed:  legacyAllowed,
		Outcome:        outcome,
		Source:         source,
		Reason:         reason,
	}
	s.metrics.RecordMandatoryAuthzShadow(ctx, shadow)
	trace.SpanFromContext(ctx).AddEvent("mandatory_authz_shadow", trace.WithAttributes(
		attr.MandatoryAuthzSurface(surface),
		attr.MandatoryAuthzCaller(shadow.Caller),
		attr.MandatoryAuthzPrincipal(shadow.Principal),
		attr.MandatoryAuthzLegacyAllowed(legacyAllowed),
		attr.MandatoryAuthzOutcome(outcome),
		attr.MandatoryAuthzSource(source),
		attr.MandatoryAuthzReason(reason),
		attribute.String(string(attr.ResourceIDKey), resourceID),
	))

	agreed := err == nil && skipped == "" && decision.Allowed == legacyAllowed
	fields := []any{
		attr.SlogOrganizationID(m.organizationID),
		attr.SlogMandatoryAuthzSurface(surface),
		attr.SlogMandatoryAuthzCaller(shadow.Caller),
		attr.SlogMandatoryAuthzPrincipal(shadow.Principal),
		attr.SlogMandatoryAuthzPrincipalID(m.authorization.PrincipalID()),
		attr.SlogMandatoryAuthzLegacyAllowed(legacyAllowed),
		attr.SlogMandatoryAuthzOutcome(outcome),
		attr.SlogMandatoryAuthzSource(source),
		attr.SlogMandatoryAuthzReason(reason),
		attr.SlogResourceID(resourceID),
		attr.SlogProjectID(projectID),
	}
	if agreed {
		s.logger.DebugContext(ctx, "mandatory authz shadow agreement", fields...)
		return
	}
	key := strings.Join([]string{m.organizationID, surface, shadow.Caller, shadow.Principal, m.authorization.PrincipalID(), outcome, source, reason, resourceID}, "|")
	level := slog.LevelInfo
	if !s.mandatoryShadowLog.first(key) {
		level = slog.LevelDebug
	}
	if err != nil {
		if level == slog.LevelInfo {
			level = slog.LevelWarn
		}
		s.logger.Log(ctx, level, "mandatory authz shadow failed", append(fields, attr.SlogError(err))...)
		return
	}
	s.logger.Log(ctx, level, "mandatory authz shadow disagreement", fields...)
}
