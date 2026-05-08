package authz

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	authzrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
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

// Log writes the challenge to ClickHouse via the supplied connection. The
// write is gated behind the ChallengeLoggingEnabled feature check — if the
// feature is not enabled for the org (or the check fails), the call is a
// no-op. Errors are logged at warn level and never bubble back to the caller.
func (l challengeLogger) Log(ctx context.Context, conn clickhouse.Conn, logger *slog.Logger, isEnabled ChallengeLoggingEnabled) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
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

	reqs := make([]authzrepo.RequestedCheck, 0, len(l.Checks))
	for _, c := range l.Checks {
		kind := c.ResourceKind
		if kind == "" {
			kind = ResourceKindForScope(c.Scope)
		}
		reqs = append(reqs, authzrepo.RequestedCheck{
			Scope:        string(c.Scope),
			ResourceKind: kind,
			ResourceID:   c.ResourceID,
			Selector:     marshalSelector(c.selector()),
		})
	}

	mgs := make([]authzrepo.MatchedGrant, 0, len(l.Matches))
	for _, m := range l.Matches {
		mgs = append(mgs, authzrepo.MatchedGrant{
			PrincipalURN:         m.Grant.PrincipalUrn,
			Scope:                string(m.Grant.Scope),
			Selector:             marshalSelector(m.Grant.Selector),
			MatchedViaCheckScope: string(m.ViaCheck.Scope),
		})
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

	row := authzrepo.ChallengeRow{
		ID:                   uuid.NewString(),
		Timestamp:            time.Now().UTC(),
		OrganizationID:       authCtx.ActiveOrganizationID,
		ProjectID:            projectID,
		TraceID:              traceID,
		SpanID:               spanID,
		RequestID:            reqID,
		PrincipalURN:         principalURN,
		PrincipalType:        principalType,
		UserID:               conv.PtrEmpty(authCtx.UserID),
		UserExternalID:       conv.PtrEmpty(authCtx.ExternalUserID),
		UserEmail:            authCtx.Email,
		APIKeyID:             conv.PtrEmpty(authCtx.APIKeyID),
		SessionID:            authCtx.SessionID,
		RoleSlugs:            roleSlugsFromContext(ctx),
		Operation:            l.Operation,
		Outcome:              l.Outcome,
		Reason:               l.Reason,
		Scope:                string(focus.Scope),
		ResourceKind:         focusSelector["resource_kind"],
		ResourceID:           focus.ResourceID,
		Selector:             marshalSelector(focusSelector),
		ExpandedScopes:       expanded,
		RequestedChecks:      reqs,
		MatchedGrants:        mgs,
		EvaluatedGrantCount:  l.EvaluatedGrantCount,
		FilterCandidateCount: l.FilterCandidateCount,
		FilterAllowedCount:   l.FilterAllowedCount,
	}

	if err := authzrepo.New(conn).InsertChallenge(ctx, row); err != nil {
		logger.WarnContext(ctx, "failed to write authz challenge row",
			attr.SlogError(err),
		)
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
