package authz

import (
	"context"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type grantMatch struct {
	Grant    Grant
	ViaCheck Check
}

type challengeLogger struct {
	logger          *slog.Logger
	operation       string
	outcome         string
	reason          string
	matchingGrants  []grantMatch
	requestedChecks []Check
	filter          *filterLog
}

func (l challengeLogger) LogChallenge(ctx context.Context) {
	attrs := authzLogAttrs(ctx, l.operation, l.outcome, l.reason)
	attrs = append(attrs,
		attr.SlogAuthzRequestedCheckCount(len(l.requestedChecks)),
		attr.SlogAuthzRequestedChecks(checksLogValue(l.requestedChecks)),
	)

	var focus *Check
	if len(l.matchingGrants) > 0 {
		c := l.matchingGrants[0].ViaCheck
		focus = &c
	} else if len(l.requestedChecks) > 0 {
		focus = &l.requestedChecks[0]
	}
	if focus != nil {
		attrs = append(attrs,
			attr.SlogAuthzScope(string(focus.Scope)),
			attr.SlogAuthzResourceKind(focus.selector()["resource_kind"]),
			attr.SlogAuthzResourceID(focus.ResourceID),
			attr.SlogAuthzSelector(focus.selector()),
			attr.SlogAuthzExpandedScopes(expandedScopeLogValue(*focus)),
		)
	}

	if len(l.matchingGrants) > 0 {
		attrs = append(attrs, attr.SlogAuthzMatchingRules(grantsLogValue(l.matchingGrants)))
	}

	l.logger.LogAttrs(ctx, slog.LevelInfo, "authz challenge result", attrs...)
}

type filterLog struct {
	CandidateCount int
	AllowedCount   int
}

func (l challengeLogger) LogFilter(ctx context.Context) {
	if l.filter == nil {
		return
	}

	attrs := authzLogAttrs(ctx, l.operation, l.outcome, l.reason)
	attrs = append(attrs,
		attr.SlogAuthzCandidateCount(l.filter.CandidateCount),
		attr.SlogAuthzAllowedCount(l.filter.AllowedCount),
		attr.SlogAuthzDeniedCount(l.filter.CandidateCount-l.filter.AllowedCount),
	)

	l.logger.LogAttrs(ctx, slog.LevelInfo, "authz filter result", attrs...)
}

func authzLogAttrs(ctx context.Context, operation, outcome, reason string) []slog.Attr {
	attrs := []slog.Attr{
		attr.SlogEvent("authz." + operation),
		attr.SlogOutcome(outcome),
		attr.SlogReason(reason),
		attr.SlogAuthzOperation(operation),
	}

	if authCtx, ok := contextvalues.GetAuthContext(ctx); ok && authCtx != nil {
		principal := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID).String()
		if authCtx.APIKeyID != "" {
			principal = "api_key:" + authCtx.APIKeyID
		}

		attrs = append(attrs,
			attr.SlogAuthOrganizationID(authCtx.ActiveOrganizationID),
			attr.SlogAuthUserID(authCtx.UserID),
			attr.SlogAuthUserExternalID(authCtx.ExternalUserID),
			attr.SlogAuthAccountType(authCtx.AccountType),
			attr.SlogAuthzPrincipal(principal),
		)
		if authCtx.APIKeyID != "" {
			attrs = append(attrs, attr.SlogAuthAPIKeyID(authCtx.APIKeyID))
		}
		if authCtx.SessionID != nil {
			attrs = append(attrs, attr.SlogAuthSessionID(*authCtx.SessionID))
		}
		if authCtx.Email != nil {
			attrs = append(attrs, attr.SlogAuthUserEmail(*authCtx.Email))
		}
		if authCtx.ProjectID != nil {
			attrs = append(attrs, attr.SlogAuthProjectID(authCtx.ProjectID.String()))
		}
	}

	if assistant, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		attrs = append(attrs,
			attr.SlogAuthzAssistantID(assistant.AssistantID.String()),
			attr.SlogAuthzAssistantThreadID(assistant.ThreadID.String()),
		)
	}

	return attrs
}

type authzEntryLog struct {
	Scope      string            `json:"scope"`
	Selector   map[string]string `json:"selector"`
	MatchedVia string            `json:"matched_via,omitempty"`
}

func checksLogValue(checks []Check) []authzEntryLog {
	out := make([]authzEntryLog, 0, len(checks))
	for _, check := range checks {
		out = append(out, authzEntryLog{
			Scope:    string(check.Scope),
			Selector: check.selector(),
		})
	}
	return out
}

func grantsLogValue(matches []grantMatch) []authzEntryLog {
	out := make([]authzEntryLog, 0, len(matches))
	for _, m := range matches {
		out = append(out, authzEntryLog{
			Scope:      string(m.Grant.Scope),
			Selector:   m.Grant.Selector,
			MatchedVia: string(m.ViaCheck.Scope),
		})
	}
	return out
}

func expandedScopeLogValue(check Check) []string {
	expanded := check.expand()
	out := make([]string, 0, len(expanded))
	for _, candidate := range expanded {
		out = append(out, string(candidate.Scope))
	}
	return out
}
