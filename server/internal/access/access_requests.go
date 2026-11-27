package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel/trace"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	chrepo "github.com/speakeasy-api/gram/server/internal/authz/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/database"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// AccessRequestTTL is how long a request waits for a decision before it
	// lapses.
	AccessRequestTTL = 24 * time.Hour

	// maxAccessRequests bounds one list of requests.
	maxAccessRequests = 200

	// openChallengeLookback is how far before a request was made its
	// approval looks for the requester's denials to resolve.
	openChallengeLookback = 7 * 24 * time.Hour
	// maxResolvedChallenges bounds the denials one approval resolves.
	maxResolvedChallenges = 50
	// openChallengePageSize and openChallengePages bound how many of the
	// requester's denials an approval reads while looking for open ones.
	openChallengePageSize = 100
	openChallengePages    = 5

	AccessRequestStatusPending   = "pending"
	AccessRequestStatusApproved  = "approved"
	AccessRequestStatusDenied    = "denied"
	AccessRequestStatusExpired   = "expired"
	AccessRequestStatusWithdrawn = "withdrawn"
)

// accessRequestStatuses are the statuses a request can be listed by.
var accessRequestStatuses = []string{AccessRequestStatusPending, AccessRequestStatusApproved, AccessRequestStatusDenied, AccessRequestStatusExpired, AccessRequestStatusWithdrawn}

// AccessRequestDecidedVia names where a decision on an access request came
// from.
type AccessRequestDecidedVia string

const (
	AccessRequestDecidedViaDashboard AccessRequestDecidedVia = "dashboard"
	AccessRequestDecidedViaSlack     AccessRequestDecidedVia = "slack"
)

var (
	// ErrAccessRequestNotFound reports a request that does not exist in the
	// organization.
	ErrAccessRequestNotFound = errors.New("access request not found")
	// ErrAccessRequestNotPending reports a request that was already decided,
	// withdrawn or has lapsed.
	ErrAccessRequestNotPending = errors.New("access request is no longer pending")
	// ErrAccessRequestSelfDecision reports a requester deciding their own
	// request.
	ErrAccessRequestSelfDecision = errors.New("the requester cannot decide their own access request")
	// ErrAccessRequestNotAdmin reports a decider who is not an organization
	// administrator.
	ErrAccessRequestNotAdmin = errors.New("only organization administrators can decide access requests")
)

// WithFeatures sets the feature flag provider that gates access requests.
// Without one, access requests stay off and requestAccess only emails
// administrators.
func (s *Service) WithFeatures(features feature.Provider) *Service {
	s.features = features
	return s
}

// AccessRequestsEnabled reports whether the access requests rollout is on for
// the organization. It fails closed.
func (s *Service) AccessRequestsEnabled(ctx context.Context, organizationID, organizationSlug string) bool {
	if s.features == nil {
		return false
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.features, feature.FlagAccessRequests, organizationID, feature.OrgProjectGroups(organizationSlug, ""))
	if err != nil {
		s.logger.WarnContext(ctx, "evaluate access requests flag", attr.SlogOrganizationID(organizationID), attr.SlogError(err))
	}
	return evaluation == feature.EvaluationEnabled
}

// storedAccessRequest is the request requestAccess recorded, and whether it is
// new or the requester's existing pending request for the same access.
type storedAccessRequest struct {
	view    *gen.AccessRequest
	created bool
}

// recordAccessRequest stores a pending request when the rollout is on for the
// organization. It returns nil when requests are not stored: the rollout is
// off, or the caller does not act for a member (any other API key, or an
// agent), who keep the email-only behaviour. For members, a scope that cannot be given
// temporarily is refused, since an approval could not grant it.
//
// member says whether the caller acts for a member: a signed-in member, or a
// device agent's per-user key whose owner the agent service has verified.
func (s *Service) recordAccessRequest(ctx context.Context, ac *contextvalues.AuthContext, payload *gen.RequestAccessPayload, member bool) (*storedAccessRequest, error) {
	if !s.AccessRequestsEnabled(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug) {
		return nil, nil
	}
	logger := s.logger.With(attr.SlogOrganizationID(ac.ActiveOrganizationID), attr.SlogUserID(ac.UserID))

	if !member {
		return nil, nil
	}
	scope := authz.Scope(payload.Scope)
	if err := ValidateTemporaryGrantScope(scope); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s cannot be requested; ask an organization administrator to assign it", payload.Scope).LogError(ctx, logger)
	}

	resourceID := strings.TrimSpace(conv.PtrValOr(payload.ResourceID, ""))
	if resourceID == "" {
		resourceID = authz.WildcardResource
	}
	selector := authz.NewSelector(scope, resourceID)
	if err := authz.ValidateSelector(scope, selector); err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid resource: %s", err.Error()).LogError(ctx, logger)
	}
	selectorJSON, err := json.Marshal(selector)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "marshal selector").LogError(ctx, logger)
	}
	now := time.Now()
	duration, until, err := RequestedAccessWindow(payload.Duration, payload.RequestedUntil, now)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "%s", err.Error()).LogWarn(ctx, logger)
	}
	requestedSeconds, requestedUntil := requestedWindowColumns(duration, until)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	if err := queries.ExpireLapsedAccessRequest(ctx, repo.ExpireLapsedAccessRequestParams{
		OrganizationID:  ac.ActiveOrganizationID,
		RequesterUserID: ac.UserID,
		Scope:           string(scope),
		Selector:        selectorJSON,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "expire lapsed access request").LogError(ctx, logger)
	}

	inserted, err := queries.InsertAccessRequest(ctx, repo.InsertAccessRequestParams{
		OrganizationID:           ac.ActiveOrganizationID,
		RequesterUserID:          ac.UserID,
		Scope:                    string(scope),
		Selector:                 selectorJSON,
		ResourceName:             conv.PtrToPGTextEmpty(trimmedPtr(payload.ResourceName)),
		Message:                  conv.PtrToPGTextEmpty(trimmedPtr(payload.Message)),
		ChallengeIds:             []string{},
		RequestedDurationSeconds: requestedSeconds,
		RequestedUntil:           requestedUntil,
		ExpiresAt:                pgtype.Timestamptz{Time: now.Add(AccessRequestTTL), InfinityModifier: pgtype.Finite, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Asking again while a request is pending returns that request, so
		// administrators are not notified twice.
		existing, err := queries.GetPendingAccessRequest(ctx, repo.GetPendingAccessRequestParams{
			OrganizationID:  ac.ActiveOrganizationID,
			RequesterUserID: ac.UserID,
			Scope:           string(scope),
			Selector:        selectorJSON,
		})
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "get pending access request").LogError(ctx, logger)
		}
		view, err := s.accessRequestView(ctx, dbtx, existing, noGrantExpiry)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "render access request").LogError(ctx, logger)
		}
		if err := dbtx.Commit(ctx); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "commit access request").LogError(ctx, logger)
		}
		return &storedAccessRequest{view: view, created: false}, nil
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "store access request").LogError(ctx, logger)
	}

	view, err := s.accessRequestView(ctx, dbtx, inserted, noGrantExpiry)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "render access request").LogError(ctx, logger)
	}
	if err := s.audit.LogAccessRequestCreate(ctx, dbtx, audit.LogAccessRequestCreateEvent{
		OrganizationID:             ac.ActiveOrganizationID,
		Actor:                      urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:           ac.Email,
		ActorSlug:                  nil,
		AccessRequestURN:           urn.NewAccessRequest(inserted.ID),
		AccessRequestSnapshotAfter: view,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log access request").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit access request").LogError(ctx, logger)
	}
	return &storedAccessRequest{view: view, created: true}, nil
}

// requestedWindowColumns turns what RequestedAccessWindow read into the
// stored columns. It does no checks of its own: RequestedAccessWindow already
// refused a duration and a deadline together, and a deadline that is past or
// more than a day away.
func requestedWindowColumns(duration time.Duration, until time.Time) (pgtype.Int4, pgtype.Timestamptz) {
	seconds := pgtype.Int4{Int32: 0, Valid: false}
	if duration > 0 {
		seconds = pgtype.Int4{Int32: conv.SafeInt32(int(duration / time.Second)), Valid: true}
	}
	deadline := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
	if !until.IsZero() {
		deadline = pgtype.Timestamptz{Time: until.UTC(), InfinityModifier: pgtype.Finite, Valid: true}
	}
	return seconds, deadline
}

// isMemberCaller reports a signed-in member acting for themselves: not an API
// key and not an agent.
func isMemberCaller(ctx context.Context, ac *contextvalues.AuthContext) bool {
	if ac.UserID == "" || ac.APIKeyID != "" {
		return false
	}
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type != urn.PrincipalTypeUser {
		return false
	}
	return true
}

func trimmedPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ListAccessRequests lists the organization's requests for an administrator,
// and a member's own requests for anyone else.
func (s *Service) ListAccessRequests(ctx context.Context, payload *gen.ListAccessRequestsPayload) (*gen.ListAccessRequestsResult, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.OrganizationID(ac.ActiveOrganizationID), attr.UserID(ac.UserID))
	logger := s.logger.With(attr.SlogOrganizationID(ac.ActiveOrganizationID), attr.SlogUserID(ac.UserID))
	if !s.AccessRequestsEnabled(ctx, ac.ActiveOrganizationID, ac.OrganizationSlug) {
		return nil, oops.C(oops.CodeNotFound)
	}

	if payload.Status != nil && !slices.Contains(accessRequestStatuses, *payload.Status) {
		return nil, oops.E(oops.CodeBadRequest, nil, "status must be one of %s", strings.Join(accessRequestStatuses, ", ")).LogError(ctx, logger)
	}

	requester := pgtype.Text{String: ac.UserID, Valid: true}
	if !payload.Mine {
		decision, err := s.authz.Decide(ctx, authz.Org(authz.OrgIDFrom(ac.ActiveOrganizationID)).Admin())
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "check organization admin").LogError(ctx, logger)
		}
		if decision.AllAllowed {
			requester = pgtype.Text{String: "", Valid: false}
		}
	}

	rows, err := repo.New(s.db).ListAccessRequests(ctx, repo.ListAccessRequestsParams{
		OrganizationID:  ac.ActiveOrganizationID,
		RequesterUserID: requester,
		Status:          conv.PtrToPGTextEmpty(payload.Status),
		MaxRows:         maxAccessRequests,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list access requests").LogError(ctx, logger)
	}
	views, err := s.accessRequestViews(ctx, s.db, rows)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "render access requests").LogError(ctx, logger)
	}
	return &gen.ListAccessRequestsResult{Requests: views}, nil
}

// DecideAccessRequest approves or denies a request from the dashboard.
func (s *Service) DecideAccessRequest(ctx context.Context, payload *gen.DecideAccessRequestPayload) (*gen.AccessRequest, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	if err := s.requireTemporaryGrantAdmin(ctx, ac.ActiveOrganizationID); err != nil {
		return nil, err
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.OrganizationID(ac.ActiveOrganizationID), attr.UserID(ac.UserID))
	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid access request id").LogError(ctx, s.logger)
	}
	if payload.Approve && conv.PtrValOr(payload.ExpiresAt, "") == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "expires_at is required to approve").LogError(ctx, s.logger)
	}

	decided, err := s.DecideAccessRequestAs(ctx, DecideAccessRequestInput{
		OrganizationID: ac.ActiveOrganizationID,
		RequestID:      requestID,
		DeciderUserID:  ac.UserID,
		DeciderEmail:   ac.Email,
		Approve:        payload.Approve,
		GrantExpiresAt: conv.PtrValOr(payload.ExpiresAt, ""),
		Via:            AccessRequestDecidedViaDashboard,
	})
	switch {
	case errors.Is(err, ErrAccessRequestNotFound):
		return nil, oops.E(oops.CodeNotFound, err, "access request not found").LogError(ctx, s.logger)
	case errors.Is(err, ErrAccessRequestNotPending):
		return nil, oops.E(oops.CodeConflict, err, "access request was already decided, withdrawn or has expired").LogError(ctx, s.logger)
	case errors.Is(err, ErrAccessRequestSelfDecision):
		return nil, oops.E(oops.CodeForbidden, err, "you cannot decide your own access request").LogError(ctx, s.logger)
	case errors.Is(err, ErrAccessRequestNotAdmin):
		return nil, oops.E(oops.CodeForbidden, err, "only organization administrators can decide access requests").LogError(ctx, s.logger)
	case err != nil:
		return nil, err
	}
	return decided, nil
}

// DecideAccessRequestInput is one decision on an access request.
type DecideAccessRequestInput struct {
	// OrganizationID is the organization the request belongs to.
	OrganizationID string

	// RequestID is the request.
	RequestID uuid.UUID

	// DeciderUserID is the administrator deciding. They are checked against
	// the organization's live grants, so a click that arrives without a
	// session, such as one from Slack, is held to the same rule.
	DeciderUserID string

	// DeciderEmail names the decider in the audit log.
	DeciderEmail *string

	// Approve gives the access; false denies the request.
	Approve bool

	// GrantExpiresAt is the RFC 3339 deadline of the temporary grant an
	// approval gives. Ignored on a denial.
	GrantExpiresAt string

	// Via is where the decision came from.
	Via AccessRequestDecidedVia
}

// DecideAccessRequestAs is the only path that decides an access request: the
// dashboard and Slack both call it. It refuses a request that is not pending,
// a requester deciding their own request and a decider who is not an
// organization administrator. An approval gives the access through the same
// temporary grant path as createTemporaryGrant, so its limits, conflict
// checks, challenge resolution and audit events apply unchanged.
func (s *Service) DecideAccessRequestAs(ctx context.Context, input DecideAccessRequestInput) (*gen.AccessRequest, error) {
	logger := s.logger.With(attr.SlogOrganizationID(input.OrganizationID), attr.SlogUserID(input.DeciderUserID))

	current, err := repo.New(s.db).GetAccessRequest(ctx, repo.GetAccessRequestParams{OrganizationID: input.OrganizationID, ID: input.RequestID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAccessRequestNotFound
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "get access request").LogError(ctx, logger)
	}
	if EffectiveAccessRequestStatus(current.AccessRequest, time.Now()) != AccessRequestStatusPending {
		return nil, ErrAccessRequestNotPending
	}
	if current.AccessRequest.RequesterUserID == input.DeciderUserID {
		return nil, ErrAccessRequestSelfDecision
	}
	admin, err := isOrganizationAdmin(ctx, s.db, input.OrganizationID, input.DeciderUserID)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "check decider is an administrator").LogError(ctx, logger)
	}
	if !admin {
		return nil, ErrAccessRequestNotAdmin
	}

	var prepared preparedTemporaryGrant
	if input.Approve {
		selector, err := authz.SelectorFromRow(current.AccessRequest.Selector)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read access request selector").LogError(ctx, logger)
		}
		challengeIDs := current.AccessRequest.ChallengeIds
		if len(challengeIDs) == 0 {
			challengeIDs = s.openChallengesForRequest(ctx, current.AccessRequest, selector)
		}
		prepared, err = s.prepareTemporaryGrant(ctx, input.OrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, current.AccessRequest.RequesterUserID).String(), authz.Scope(current.AccessRequest.Scope), selector, input.GrantExpiresAt, challengeIDs)
		if err != nil {
			return nil, err
		}
	}
	answeredIDs := current.AccessRequest.ChallengeIds
	if input.Approve {
		answeredIDs = make([]string, 0, len(prepared.answered))
		for _, challenge := range prepared.answered {
			answeredIDs = append(answeredIDs, challenge.ID)
		}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	locked, err := queries.GetAccessRequestForUpdate(ctx, repo.GetAccessRequestForUpdateParams{OrganizationID: input.OrganizationID, ID: input.RequestID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAccessRequestNotFound
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock access request").LogError(ctx, logger)
	}
	if EffectiveAccessRequestStatus(locked, time.Now()) != AccessRequestStatusPending {
		return nil, ErrAccessRequestNotPending
	}
	before, err := s.accessRequestView(ctx, dbtx, locked, noGrantExpiry)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "render access request").LogError(ctx, logger)
	}

	actor := urn.NewPrincipal(urn.PrincipalTypeUser, input.DeciderUserID)
	status := AccessRequestStatusDenied
	grantID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if input.Approve {
		reason := fmt.Sprintf("Approved access request %s", input.RequestID)
		grant, err := s.grantTemporary(ctx, dbtx, prepared, actor, input.DeciderEmail, reason)
		if err != nil {
			return nil, err
		}
		parsed, err := uuid.Parse(grant.ID)
		if err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "read temporary grant id").LogError(ctx, logger)
		}
		status = AccessRequestStatusApproved
		grantID = uuid.NullUUID{UUID: parsed, Valid: true}
	}

	if _, err := queries.DecideAccessRequest(ctx, repo.DecideAccessRequestParams{
		Status:           status,
		DecidedByUserID:  pgtype.Text{String: input.DeciderUserID, Valid: true},
		DecidedVia:       pgtype.Text{String: string(input.Via), Valid: true},
		TemporaryGrantID: grantID,
		ChallengeIds:     answeredIDs,
		OrganizationID:   input.OrganizationID,
		ID:               input.RequestID,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "record access request decision").LogError(ctx, logger)
	}
	decided, err := queries.GetAccessRequest(ctx, repo.GetAccessRequestParams{OrganizationID: input.OrganizationID, ID: input.RequestID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "reload access request").LogError(ctx, logger)
	}
	after, err := s.accessRequestView(ctx, dbtx, decided.AccessRequest, decided.GrantExpiresAt)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "render access request").LogError(ctx, logger)
	}

	event := audit.LogAccessRequestDecideEvent{
		OrganizationID:              input.OrganizationID,
		Actor:                       actor,
		ActorDisplayName:            input.DeciderEmail,
		ActorSlug:                   nil,
		AccessRequestURN:            urn.NewAccessRequest(input.RequestID),
		AccessRequestSnapshotBefore: before,
		AccessRequestSnapshotAfter:  after,
	}
	logDecision := s.audit.LogAccessRequestDeny
	if input.Approve {
		logDecision = s.audit.LogAccessRequestApprove
	}
	if err := logDecision(ctx, dbtx, event); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log access request decision").LogError(ctx, logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit access request decision").LogError(ctx, logger)
	}
	return after, nil
}

// GetAccessRequest reads one request for a caller that has already checked
// who may see it, such as the Slack card of a DM sent to an administrator.
func (s *Service) GetAccessRequest(ctx context.Context, organizationID string, requestID uuid.UUID) (*gen.AccessRequest, error) {
	row, err := repo.New(s.db).GetAccessRequest(ctx, repo.GetAccessRequestParams{OrganizationID: organizationID, ID: requestID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAccessRequestNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get access request: %w", err)
	}
	view, err := s.accessRequestView(ctx, s.db, row.AccessRequest, row.GrantExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("render access request: %w", err)
	}
	return view, nil
}

// WithdrawAccessRequest lets a requester take back a pending request.
func (s *Service) WithdrawAccessRequest(ctx context.Context, payload *gen.WithdrawAccessRequestPayload) (*gen.AccessRequest, error) {
	ac, err := s.authContext(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnauthorized, err, "missing auth context").LogError(ctx, s.logger)
	}
	trace.SpanFromContext(ctx).SetAttributes(attr.OrganizationID(ac.ActiveOrganizationID), attr.UserID(ac.UserID))
	logger := s.logger.With(attr.SlogOrganizationID(ac.ActiveOrganizationID), attr.SlogUserID(ac.UserID))
	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid access request id").LogError(ctx, logger)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "begin transaction").LogError(ctx, logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })
	queries := repo.New(dbtx)

	locked, err := queries.GetAccessRequestForUpdate(ctx, repo.GetAccessRequestForUpdateParams{OrganizationID: ac.ActiveOrganizationID, ID: requestID})
	// Another member's request reads as missing, so ids cannot be probed.
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && locked.RequesterUserID != ac.UserID) {
		return nil, oops.E(oops.CodeNotFound, nil, "access request not found").LogError(ctx, logger)
	}
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "lock access request").LogError(ctx, logger)
	}
	if EffectiveAccessRequestStatus(locked, time.Now()) != AccessRequestStatusPending {
		return nil, oops.E(oops.CodeConflict, nil, "access request was already decided, withdrawn or has expired").LogError(ctx, logger)
	}
	before, err := s.accessRequestView(ctx, dbtx, locked, noGrantExpiry)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "render access request").LogError(ctx, logger)
	}
	withdrawn, err := queries.WithdrawAccessRequest(ctx, repo.WithdrawAccessRequestParams{OrganizationID: ac.ActiveOrganizationID, ID: requestID, RequesterUserID: ac.UserID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "withdraw access request").LogError(ctx, logger)
	}
	after, err := s.accessRequestView(ctx, dbtx, withdrawn, noGrantExpiry)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "render access request").LogError(ctx, logger)
	}
	if err := s.audit.LogAccessRequestWithdraw(ctx, dbtx, audit.LogAccessRequestWithdrawEvent{
		OrganizationID:              ac.ActiveOrganizationID,
		Actor:                       urn.NewPrincipal(urn.PrincipalTypeUser, ac.UserID),
		ActorDisplayName:            ac.Email,
		ActorSlug:                   nil,
		AccessRequestURN:            urn.NewAccessRequest(requestID),
		AccessRequestSnapshotBefore: before,
		AccessRequestSnapshotAfter:  after,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "log access request withdrawal").LogError(ctx, logger)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "commit access request withdrawal").LogError(ctx, logger)
	}
	return after, nil
}

// EffectiveAccessRequestStatus reports a pending request whose window has
// passed as expired, without waiting for anything to rewrite the row.
func EffectiveAccessRequestStatus(row repo.AccessRequest, now time.Time) string {
	if row.Status == AccessRequestStatusPending && !row.ExpiresAt.Time.After(now) {
		return AccessRequestStatusExpired
	}
	return row.Status
}

func (s *Service) accessRequestView(ctx context.Context, db database.DBTX, row repo.AccessRequest, grantExpiresAt pgtype.Timestamptz) (*gen.AccessRequest, error) {
	views, err := s.accessRequestViews(ctx, db, []repo.ListAccessRequestsRow{{AccessRequest: row, GrantExpiresAt: grantExpiresAt}})
	if err != nil {
		return nil, err
	}
	return views[0], nil
}

// accessRequestViews renders requests, naming every requester and decider
// with one lookup.
func (s *Service) accessRequestViews(ctx context.Context, db database.DBTX, rows []repo.ListAccessRequestsRow) ([]*gen.AccessRequest, error) {
	ids := make([]string, 0, len(rows)*2)
	for _, row := range rows {
		ids = append(ids, row.AccessRequest.RequesterUserID)
		if row.AccessRequest.DecidedByUserID.Valid {
			ids = append(ids, row.AccessRequest.DecidedByUserID.String)
		}
	}
	names := make(map[string]string, len(ids))
	if len(ids) > 0 {
		people, err := repo.New(db).ListAccessRequestPeople(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("name access request people: %w", err)
		}
		for _, person := range people {
			names[person.ID] = conv.Default(person.DisplayName, person.Email)
		}
	}

	now := time.Now()
	out := make([]*gen.AccessRequest, 0, len(rows))
	for _, item := range rows {
		row := item.AccessRequest
		selector, err := authz.SelectorFromRow(row.Selector)
		if err != nil {
			return nil, fmt.Errorf("unmarshal access request selector: %w", err)
		}
		view := &gen.AccessRequest{
			ID:                       row.ID.String(),
			RequesterUserID:          row.RequesterUserID,
			RequesterName:            conv.Default(names[row.RequesterUserID], "A former member"),
			Scope:                    row.Scope,
			Selector:                 authzSelectorToGen(selector),
			ResourceName:             conv.FromPGText[string](row.ResourceName),
			Message:                  conv.FromPGText[string](row.Message),
			RequestedDurationSeconds: nil,
			RequestedUntil:           nil,
			Status:                   EffectiveAccessRequestStatus(row, now),
			DecidedByUserID:          conv.FromPGText[string](row.DecidedByUserID),
			DecidedByName:            nil,
			DecidedVia:               conv.FromPGText[string](row.DecidedVia),
			DecidedAt:                nil,
			TemporaryGrantID:         nil,
			GrantExpiresAt:           nil,
			ExpiresAt:                row.ExpiresAt.Time.UTC().Format(time.RFC3339),
			CreatedAt:                row.CreatedAt.Time.UTC().Format(time.RFC3339),
			UpdatedAt:                row.UpdatedAt.Time.UTC().Format(time.RFC3339),
		}
		if row.RequestedDurationSeconds.Valid {
			view.RequestedDurationSeconds = new(int(row.RequestedDurationSeconds.Int32))
		}
		if row.RequestedUntil.Valid {
			view.RequestedUntil = new(row.RequestedUntil.Time.UTC().Format(time.RFC3339))
		}
		if row.DecidedByUserID.Valid {
			view.DecidedByName = new(conv.Default(names[row.DecidedByUserID.String], "A former member"))
		}
		if row.DecidedAt.Valid {
			view.DecidedAt = new(row.DecidedAt.Time.UTC().Format(time.RFC3339))
		}
		if row.TemporaryGrantID.Valid {
			view.TemporaryGrantID = new(row.TemporaryGrantID.UUID.String())
		}
		if item.GrantExpiresAt.Valid {
			view.GrantExpiresAt = new(item.GrantExpiresAt.Time.UTC().Format(time.RFC3339))
		}
		out = append(out, view)
	}
	return out, nil
}

// noGrantExpiry renders a request without the deadline of a grant it gave:
// one that is pending, denied or withdrawn has none.
var noGrantExpiry = pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}

// openChallengesForRequest finds the requester's unresolved denials that an
// approval of request answers: denies of the same scope, raised by the
// requester, on a resource the request's selector covers, from the week
// before the request was made onwards. The approval resolves them through the
// temporary grant, so authz_challenge_resolutions stays the record of which
// denials are resolved. Finding them is best effort: when ClickHouse cannot be
// read, the approval goes ahead without resolving any.
func (s *Service) openChallengesForRequest(ctx context.Context, request repo.AccessRequest, selector authz.Selector) []string {
	logger := s.logger.With(attr.SlogOrganizationID(request.OrganizationID), attr.SlogUserID(request.RequesterUserID))
	recipient := urn.NewPrincipal(urn.PrincipalTypeUser, request.RequesterUserID)
	outcome := string(chrepo.OutcomeDeny)
	principal := recipient.String()
	scope := request.Scope
	from := request.CreatedAt.Time.Add(-openChallengeLookback)
	// A fixed end keeps pages stable while new denials arrive.
	to := time.Now()
	queries := chrepo.New(s.chReadConn)
	seen := make(map[string]struct{}, maxResolvedChallenges)

	// Denials of other resources and ones already resolved are filtered out
	// after the read, so page until enough open ones are found or the window
	// runs out.
	ids := make([]string, 0, maxResolvedChallenges)
	for page := range openChallengePages {
		challenges, err := queries.ListChallenges(ctx, chrepo.ChallengeListFilters{
			ChallengeFilters: chrepo.ChallengeFilters{
				OrganizationID: request.OrganizationID,
				ProjectID:      nil,
				Outcome:        &outcome,
				PrincipalURN:   &principal,
				Scope:          &scope,
				ExcludeScopes:  nil,
				MemberUserIDs:  nil,
			},
			From:           &from,
			To:             &to,
			Limit:          openChallengePageSize,
			Offset:         uint64(page) * openChallengePageSize,
			SkipPagination: false,
		})
		if err != nil {
			logger.WarnContext(ctx, "list denials an access request answers", attr.SlogError(err))
			return nil
		}
		covered := make([]chrepo.ChallengeSummary, 0, len(challenges))
		for _, challenge := range challenges {
			if validateChallengesForTemporaryGrant([]chrepo.ChallengeSummary{challenge}, []string{challenge.ID}, recipient, authz.Scope(scope), selector) == nil {
				covered = append(covered, challenge)
			}
		}
		if len(covered) > 0 {
			resolutions, err := s.lookupResolutions(ctx, request.OrganizationID, covered)
			if err != nil {
				logger.WarnContext(ctx, "read resolutions of denials an access request answers", attr.SlogError(err))
				return nil
			}
			for _, challenge := range covered {
				if _, resolved := resolutions[challenge.ID]; resolved {
					continue
				}
				if _, dup := seen[challenge.ID]; dup {
					continue
				}
				seen[challenge.ID] = struct{}{}
				ids = append(ids, challenge.ID)
				if len(ids) == maxResolvedChallenges {
					return ids
				}
			}
		}
		if uint64(len(challenges)) < openChallengePageSize {
			break
		}
	}
	return ids
}

// isOrganizationAdmin reports whether the member holds org:admin in the
// organization, read from their live grants. It needs no session, so a
// decision from Slack is held to the same rule as one from the dashboard, and
// it is always enforced.
func isOrganizationAdmin(ctx context.Context, db database.DBTX, organizationID, userID string) (bool, error) {
	principals, err := authz.ResolveUserPrincipals(ctx, db, organizationID, userID)
	if err != nil {
		return false, fmt.Errorf("resolve principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, db, organizationID, principals)
	if err != nil {
		return false, fmt.Errorf("load grants: %w", err)
	}
	allowed, err := authz.GrantsAuthorize(grants, authz.Org(authz.OrgIDFrom(organizationID)).Admin())
	if err != nil {
		return false, fmt.Errorf("evaluate org:admin: %w", err)
	}
	return allowed, nil
}
