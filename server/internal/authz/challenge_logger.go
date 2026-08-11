package authz

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	authzv1 "github.com/speakeasy-api/gram/infra/gen/gram/authz/v1"
	"github.com/speakeasy-api/gram/server/internal/attr"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type grantMatch struct {
	Grant    Grant
	ViaCheck Check
}

// challengeLogger captures the data describing a single authz decision. The
// engine constructs one of these inline at each Require/RequireAny/Filter
// call site, then invokes Log to persist it.
type challengeLogger struct {
	Operation authzrepo.Operation
	Outcome   authzrepo.Outcome
	Reason    authzrepo.Reason

	Checks  []Check
	Focus   *Check
	Matches []grantMatch

	EvaluatedGrantCount uint32

	FilterCandidateCount uint32
	FilterAllowedCount   uint32
}

const challengeOutboxTimeout = 10 * time.Second

// Log enqueues the challenge for asynchronous ingestion into ClickHouse. The
// enqueue is gated behind the ChallengeLoggingEnabled feature check — if the
// feature is not enabled for the org (or the check fails), the call is a
// no-op. Errors are logged at warn level and never bubble back to the caller.
func (l challengeLogger) Log(ctx context.Context, dbtx database.DBTX, logger *slog.Logger, isEnabled ChallengeLoggingEnabled) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return
	}

	// Skip challenge logging when a Speakeasy admin is impersonating a
	// customer org — challenges belong to the admin, not the customer.
	if _, impersonating := contextvalues.GetAdminOverrideFromContext(ctx); impersonating {
		return
	}

	// Likewise for sessions exploring the shared demo org: challenges there
	// belong to the visitor, and the daily seed rerun would wipe them anyway.
	if authCtx.ActiveOrganizationID == constants.DemoOrganizationID {
		return
	}

	enabled, err := isEnabled(ctx, authCtx.ActiveOrganizationID)
	if err != nil {
		logger.WarnContext(ctx, "failed to check authz challenge logging feature flag",
			attr.SlogError(err),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		)
		return
	}
	if !enabled {
		return
	}

	principalURN := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID).String()
	principalType := authzrepo.PrincipalTypeUser
	if authCtx.APIKeyID != "" {
		principalURN = "api_key:" + authCtx.APIKeyID
		principalType = authzrepo.PrincipalTypeAPIKey
	}
	if _, isAssistant := contextvalues.GetAssistantPrincipal(ctx); isAssistant {
		principalType = authzrepo.PrincipalTypeAssistant
	}

	var focus Check
	var focusSelector Selector
	if l.Focus != nil {
		focus = *l.Focus
		focusSelector = focus.selector()
	}

	sc := trace.SpanContextFromContext(ctx)
	traceID := sc.TraceID().String()
	spanID := sc.SpanID().String()

	projectID := ""
	if authCtx.ProjectID != nil {
		projectID = authCtx.ProjectID.String()
	}

	reqs := make([]*authzv1.Challenge_RequestedCheck, 0, len(l.Checks))
	for _, c := range l.Checks {
		kind := c.ResourceKind
		if kind == "" {
			kind = ResourceKindForScope(c.Scope)
		}
		scope := string(c.Scope)
		resourceID := c.ResourceID
		selector := marshalSelector(c.selector())
		reqs = append(reqs, authzv1.Challenge_RequestedCheck_builder{
			Scope:        &scope,
			ResourceKind: &kind,
			ResourceId:   &resourceID,
			Selector:     &selector,
		}.Build())
	}

	mgs := make([]*authzv1.Challenge_MatchedGrant, 0, len(l.Matches))
	for _, m := range l.Matches {
		principalURN := m.Grant.PrincipalUrn
		scope := string(m.Grant.Scope)
		selector := marshalSelector(m.Grant.Selector)
		matchedViaCheckScope := string(m.ViaCheck.Scope)
		mgs = append(mgs, authzv1.Challenge_MatchedGrant_builder{
			PrincipalUrn:         &principalURN,
			Scope:                &scope,
			Selector:             &selector,
			MatchedViaCheckScope: &matchedViaCheckScope,
		}.Build())
	}

	expanded := []string{}
	if focus.Scope != "" {
		for _, c := range focus.expand() {
			expanded = append(expanded, string(c.Scope))
		}
	}

	var reqID *string
	if rc, ok := contextvalues.GetRequestContext(ctx); ok && rc != nil && rc.ReqID != "" {
		v := rc.ReqID
		reqID = &v
	}

	challengeID := uuid.New()
	id := challengeID.String()
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	principalTypeString := string(principalType)
	operation := string(l.Operation)
	outcome := string(l.Outcome)
	reason := string(l.Reason)
	scope := string(focus.Scope)
	resourceKind := focusSelector[SelectorKeyResourceKind]
	resourceID := focus.ResourceID
	selector := marshalSelector(focusSelector)
	message := authzv1.Challenge_builder{
		Id:                   &id,
		Timestamp:            &timestamp,
		OrganizationId:       &authCtx.ActiveOrganizationID,
		ProjectId:            &projectID,
		TraceId:              &traceID,
		SpanId:               &spanID,
		RequestId:            reqID,
		PrincipalUrn:         &principalURN,
		PrincipalType:        &principalTypeString,
		UserId:               conv.PtrEmpty(authCtx.UserID),
		UserExternalId:       conv.PtrEmpty(authCtx.ExternalUserID),
		UserEmail:            authCtx.Email,
		ApiKeyId:             conv.PtrEmpty(authCtx.APIKeyID),
		SessionId:            authCtx.SessionID,
		RoleSlugs:            roleSlugsFromContext(ctx),
		Operation:            &operation,
		Outcome:              &outcome,
		Reason:               &reason,
		Scope:                &scope,
		ResourceKind:         &resourceKind,
		ResourceId:           &resourceID,
		Selector:             &selector,
		ExpandedScopes:       expanded,
		RequestedChecks:      reqs,
		MatchedGrants:        mgs,
		EvaluatedGrantCount:  &l.EvaluatedGrantCount,
		FilterCandidateCount: &l.FilterCandidateCount,
		FilterAllowedCount:   &l.FilterAllowedCount,
	}.Build()

	// Request cancellation must not abort the durable enqueue after the
	// authorization decision has completed. Keep it bounded so challenge
	// logging cannot hold up the request indefinitely during a database outage.
	enqueueCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), challengeOutboxTimeout)
	defer cancel()

	if _, err := outbox.Publish(enqueueCtx, dbtx, authCtx.ActiveOrganizationID, outbox.Message{
		Proto:      message,
		PublicID:   challengeID,
		Attributes: nil,
	}); err != nil {
		logger.WarnContext(enqueueCtx, "failed to enqueue authz challenge",
			attr.SlogError(err),
		)
		return
	}
}

func marshalSelector(v Selector) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func roleSlugsFromContext(ctx context.Context) []string {
	grants, ok := GrantsFromContext(ctx)
	if !ok {
		return []string{}
	}
	const prefix = "role:"
	seen := map[string]struct{}{}
	out := []string{}
	for _, g := range grants {
		if !strings.HasPrefix(g.PrincipalUrn, prefix) {
			continue
		}
		slug := g.PrincipalUrn[len(prefix):]
		if _, dup := seen[slug]; dup {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	return out
}
