package authz

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
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
// engine constructs one of these inline at each Require/Decide
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

const challengeLogStepTimeout = 10 * time.Second

// Log enqueues the challenge for asynchronous ingestion into ClickHouse. The
// enqueue is gated behind the ChallengeLoggingEnabled feature check — if the
// feature is not enabled for the org (or the check fails), the call is a
// no-op. Errors are logged at warn level and never bubble back to the caller.
// It reports whether the challenge was enqueued.
func (l challengeLogger) Log(ctx context.Context, dbtx database.DBTX, logger *slog.Logger, isEnabled ChallengeLoggingEnabled) bool {
	RecordAuthorizationDecision(ctx, l.Operation, l.Outcome, l.Reason)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return false
	}

	// Skip challenge logging when a Speakeasy admin is impersonating a
	// customer org — challenges belong to the admin, not the customer.
	if contextvalues.IsSupportSession(ctx) {
		return false
	}

	// Likewise for sessions exploring the shared demo org: challenges there
	// belong to the visitor, and the daily seed rerun would wipe them anyway.
	if authCtx.ActiveOrganizationID == constants.DemoOrganizationID {
		return false
	}

	// A listing records nothing (see WithoutChallenges).
	if challengesSilenced(ctx) {
		return false
	}

	// Detached and bounded: a client cancel must not drop a completed decision.
	checkCtx, cancelCheck := context.WithTimeout(context.WithoutCancel(ctx), challengeLogStepTimeout)
	defer cancelCheck()

	enabled, err := isEnabled(checkCtx, authCtx.ActiveOrganizationID)
	if err != nil {
		logger.WarnContext(checkCtx, "failed to check authz challenge logging feature flag",
			attr.SlogError(err),
			attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		)
		return false
	}
	if !enabled {
		return false
	}
	// A request records each resource it names once (see
	// WithOneChallengePerResource).
	if !claimChallenge(ctx, l) {
		return false
	}

	principalURN, principalType := authorizationChallengePrincipal(ctx, authCtx)

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

	enqueueCtx, cancelEnqueue := context.WithTimeout(context.WithoutCancel(ctx), challengeLogStepTimeout)
	defer cancelEnqueue()

	if _, err := outbox.Publish(enqueueCtx, dbtx, authCtx.ActiveOrganizationID, outbox.Message{
		Proto:      message,
		PublicID:   challengeID,
		Attributes: nil,
	}); err != nil {
		logger.WarnContext(enqueueCtx, "failed to enqueue authz challenge",
			attr.SlogError(err),
		)
		return false
	}
	return true
}

func authorizationChallengePrincipal(ctx context.Context, authCtx *contextvalues.AuthContext) (string, authzrepo.PrincipalType) {
	principalURN := urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID).String()
	principalType := authzrepo.PrincipalTypeUser
	if authCtx.APIKeyID != "" {
		principalURN = "api_key:" + authCtx.APIKeyID
		principalType = authzrepo.PrincipalTypeAPIKey
	}
	if _, isAssistant := contextvalues.GetAssistantPrincipal(ctx); isAssistant {
		principalType = authzrepo.PrincipalTypeAssistant
	}
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type == urn.PrincipalTypeAgent {
		principalURN = actor.String()
		principalType = authzrepo.PrincipalTypeAgent
	}
	// A workload actor carries neither a UserID nor an APIKeyID, so without
	// this arm its challenges would be logged against an empty user principal.
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type == urn.PrincipalTypeWorkload {
		principalURN = actor.String()
		principalType = authzrepo.PrincipalTypeWorkload
	}
	return principalURN, principalType
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

type challengeScopeKey struct{}

// challengeScope decides which decisions of one request are recorded as
// authorization checks. Decisions still count in the decision metrics either
// way; only the recorded check, an outbox row each, is skipped.
type challengeScope struct {
	// silent records nothing: the caller is filtering or listing, not
	// asking for one resource.
	silent bool
	// recorded is shared by every scope derived from one request. Nil for a
	// silent scope.
	recorded *recordedChallenges
}

// recordedChallenges remembers which decisions a request has recorded.
type recordedChallenges struct {
	mu sync.Mutex
	// decided holds every scope and resource with a recorded decision.
	decided map[challengeResource]struct{}
	// denied holds every recorded denial by the tool and disposition it
	// refused, so each distinct refusal on a resource is recorded once.
	denied map[challengeDenial]struct{}
}

type challengeResource struct {
	scope      Scope
	resourceID string
}

// challengeDenial is the refusal of one tool and disposition on a resource. A
// check that names no tool, such as connecting to the server, has both empty.
type challengeDenial struct {
	resource    challengeResource
	tool        string
	disposition string
	// server is the MCP server an auth client was refused on: one client
	// serves several servers, and each refusal is its own.
	server string
}

// WithOneChallengePerResource returns a context under which the engine records
// at most one check per scope and resource for the rest of the request: the
// first decision, and later each distinct denial (by tool and disposition) not
// recorded yet. A gateway request checks the server it names several times
// (the server, then the tool it calls), and every one of those would otherwise
// be its own row.
func WithOneChallengePerResource(ctx context.Context) context.Context {
	if scope, ok := ctx.Value(challengeScopeKey{}).(*challengeScope); ok && scope.recorded != nil {
		return ctx
	}
	return context.WithValue(ctx, challengeScopeKey{}, &challengeScope{
		silent:   false,
		recorded: &recordedChallenges{mu: sync.Mutex{}, decided: map[challengeResource]struct{}{}, denied: map[challengeDenial]struct{}{}},
	})
}

// WithoutChallenges returns a context under which the engine records no
// checks. Use it where a request filters or lists resources it did not name,
// so a caller is not shown a denial for every resource it simply cannot see.
func WithoutChallenges(ctx context.Context) context.Context {
	return context.WithValue(ctx, challengeScopeKey{}, &challengeScope{silent: true, recorded: nil})
}

// challengesSilenced reports a context that records no checks. It is read
// before anything else a recorded check costs, such as the feature flag.
func challengesSilenced(ctx context.Context) bool {
	scope, ok := ctx.Value(challengeScopeKey{}).(*challengeScope)
	return ok && scope.silent
}

// claimChallenge reports whether a decision is recorded under ctx's scope, and
// marks it recorded. Call it only once the decision is about to be written.
func claimChallenge(ctx context.Context, l challengeLogger) bool {
	scope, ok := ctx.Value(challengeScopeKey{}).(*challengeScope)
	if !ok {
		return true
	}
	if scope.recorded == nil {
		return false
	}

	// A batch Decide names no one resource. It never takes a
	// resource's slot, so a later decision about that resource still records,
	// but it is skipped once that resource has a recorded decision.
	batch := l.Focus == nil
	focus := l.Focus
	if batch && len(l.Checks) > 0 {
		focus = &l.Checks[0]
	}
	if focus == nil {
		return true
	}
	key := challengeResource{scope: focus.Scope, resourceID: focus.ResourceID}

	scope.recorded.mu.Lock()
	defer scope.recorded.mu.Unlock()
	_, decided := scope.recorded.decided[key]
	if batch {
		return !decided
	}
	switch l.Outcome {
	case authzrepo.OutcomeAllow:
		if decided {
			return false
		}
	case authzrepo.OutcomeDeny:
		// A denial after an allow is still recorded: connecting was allowed
		// but this tool or disposition was not, and that refusal is real. Two
		// different refused tools on one server are two refusals.
		denial := challengeDenial{
			resource:    key,
			tool:        focus.Dimensions[SelectorKeyTool],
			disposition: focus.Dimensions[SelectorKeyDisposition],
			server:      focus.Dimensions[ChallengeKeyMCPServerID],
		}
		if _, seen := scope.recorded.denied[denial]; seen {
			return false
		}
		scope.recorded.denied[denial] = struct{}{}
	default:
		// Errors are always recorded: they are not a decision about access.
		return true
	}
	scope.recorded.decided[key] = struct{}{}
	return true
}
