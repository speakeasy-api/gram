// Package approval implements Speakeasy Approvals: MCP tool calls that a
// require_approval risk policy holds until a listed approver approves them.
//
// The gateway holds a covered call and returns a request token. The agent
// files the request by posting a reason to the request endpoint with curl,
// waits on a curl loop that polls the watch endpoint with the token, and
// retries the same call once it is approved. The token is the only credential
// both endpoints need. An exact-once approval is consumed by that retry.
package approval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpidentity"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk/approval/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Status is the lifecycle state of an approval request.
type Status string

const (
	// StatusBlocked is a held call whose agent has not requested approval yet.
	StatusBlocked Status = "blocked"

	// StatusPending is a requested approval waiting for an approver.
	StatusPending Status = "pending"

	// StatusApproved is an approval not yet used by a retry.
	StatusApproved Status = "approved"

	// StatusDenied is a request an approver turned down.
	StatusDenied Status = "denied"

	// StatusConsumed is an exact-once approval that a retry has used.
	StatusConsumed Status = "consumed"

	// StatusExpired is a request nobody decided before it expired.
	StatusExpired Status = "expired"
)

// DecidedVia names where a decision came from.
type DecidedVia string

const (
	// DecidedViaSlack is a decision clicked in an approver's Slack DM.
	DecidedViaSlack DecidedVia = "slack"

	// DecidedViaDashboard is a decision made in the dashboard.
	DecidedViaDashboard DecidedVia = "dashboard"

	// DecidedViaAPI is a decision made through the management API.
	DecidedViaAPI DecidedVia = "api"
)

// ScopeExactOnce approves one retry of the exact call that was held. It is the
// default and, until always-allow rules ship, the only scope.
const ScopeExactOnce = "exact_once"

const (
	// defaultRequestTTL is how long an agent waits for a decision when it does
	// not say: long enough for an approver to see a Slack DM during the day.
	defaultRequestTTL = 30 * time.Minute

	// maxRequestTTL caps how long a request stays open, so a forgotten request
	// cannot be approved days later.
	maxRequestTTL = 24 * time.Hour

	// minRequestTTL keeps an agent from asking for a window too short for any
	// person to answer.
	minRequestTTL = time.Minute

	// maxReasonRunes bounds the agent's reason, which approvers read on a
	// Slack card: one or two sentences.
	maxReasonRunes = 500

	// maxPreviewStringRunes truncates each string argument in the preview
	// approvers see, so a long body cannot flood the card or the row.
	maxPreviewStringRunes = 200

	// maxPreviewKeys bounds how many top-level arguments the preview keeps.
	maxPreviewKeys = 20

	// omittedArgumentsKey names the preview entry that says how many
	// top-level arguments were left out. When an argument already has that
	// name, the entry takes a longer run of "…".
	omittedArgumentsKey = "…"
)

var (
	// ErrNotFound is returned for an unknown token or request, and for a token
	// presented by a principal other than the requester, so a caller cannot
	// tell those apart.
	ErrNotFound = errors.New("approval request not found")

	// ErrNotPending is returned when deciding a request that is no longer
	// waiting for a decision.
	ErrNotPending = errors.New("approval request is not pending")

	// ErrNotApprover is returned when the decider is not on the policy's
	// approver list.
	ErrNotApprover = errors.New("decider is not an approver for this policy")

	// ErrSelfApproval is returned when the person who made the call tries to
	// approve it.
	ErrSelfApproval = errors.New("the requester cannot decide their own request")

	// ErrInvalidRequest is returned for an approval request with invalid
	// arguments.
	ErrInvalidRequest = errors.New("invalid approval request")
)

// Service holds, requests, watches and decides approval requests.
type Service struct {
	logger    *slog.Logger
	db        *pgxpool.Pool
	serverURL *url.URL
	audit     *audit.Logger
	now       func() time.Time
}

// NewService creates an approval service. serverURL is the public base URL
// that request instructions point agents at.
func NewService(logger *slog.Logger, db *pgxpool.Pool, serverURL *url.URL, auditLogger *audit.Logger) *Service {
	return &Service{
		logger:    logger.With(attr.SlogComponent("risk_approval")),
		db:        db,
		serverURL: serverURL,
		audit:     auditLogger,
		now:       time.Now,
	}
}

// WatchURL is the endpoint the watcher polls.
func (s *Service) WatchURL() string {
	return s.serverURL.JoinPath(WatchPath).String()
}

// RequestURL is the endpoint an agent posts its approval request to.
func (s *Service) RequestURL() string {
	return s.serverURL.JoinPath(RequestPath).String()
}

// waitCommand is the shell loop an agent runs to wait for a decision. It polls
// the plain-text watch endpoint and prints a single line once the request is
// decided, so a Monitor wakes the agent exactly once. Transient failures print
// nothing and the loop keeps polling.
func (s *Service) waitCommand(token string) string {
	watchURL := s.serverURL.JoinPath(WatchPath)
	watchURL.RawQuery = "format=text"
	return fmt.Sprintf(`while :; do s=$(curl -s --max-time 10 -H 'Authorization: Bearer %s' '%s'); case "$s" in APPROVED*|DENIED*|EXPIRED*|BLOCKED*|INVALID*) echo "$s"; break;; esac; sleep %d; done`,
		token, watchURL.String(), watchIntervalSeconds)
}

// HoldInput is one MCP tool call a require_approval policy covers.
type HoldInput struct {
	// Policies are the require_approval policies that cover the call. The call
	// runs only once each of them has approved it.
	Policies []policycore.Policy

	// OrganizationID is the organization the call belongs to.
	OrganizationID string

	// ProjectID is the project the call belongs to.
	ProjectID uuid.UUID

	// RequesterURN is the principal that made the call: an agent or a user.
	RequesterURN string

	// RequesterUserID is set when a person called through their own session.
	RequesterUserID string

	// Surface is the MCP surface that received the call.
	Surface string

	// ToolsetID is the hosted toolset that served the call, when known.
	ToolsetID string

	// ServerID is the MCP server the call targets, when known. It is stored
	// with the request only when it names an MCP server in the call's project,
	// so approvers see which server the tool belongs to. A Platform MCP call
	// passes a toolset id here, which is not stored.
	ServerID string

	// ToolName is the called tool.
	ToolName string

	// ChatID is the chat the call belongs to, when known.
	ChatID string

	// Arguments is the raw JSON arguments object of the call.
	Arguments []byte
}

// HoldOutcome is the result of holding one call.
type HoldOutcome struct {
	// Allowed reports that an approval covered the call and was used.
	Allowed bool

	// Token is the request token the agent requests approval and watches
	// with. Empty when Allowed.
	Token string

	// UserMessage tells the agent how to request approval and resume.
	UserMessage string

	// Policy is the policy that holds the call. Nil when Allowed.
	Policy *policycore.Policy
}

// Requester maps validated MCP provenance to the principal an approval
// request binds to. Only agents and people signed in through their own session
// can request approval.
func Requester(identity mcpidentity.Identity) (requesterURN, requesterUserID string, ok bool) {
	switch identity.Kind() {
	case mcpidentity.KindAgent:
		if identity.AgentID() == "" {
			return "", "", false
		}
		return urn.NewPrincipal(urn.PrincipalTypeAgent, identity.AgentID()).String(), "", true
	case mcpidentity.KindUserSession:
		if identity.UserID() == "" {
			return "", "", false
		}
		return urn.NewPrincipal(urn.PrincipalTypeUser, identity.UserID()).String(), identity.UserID(), true
	default:
		return "", "", false
	}
}

// HoldCall lets the call through only when every policy covering it has an
// approved exact-once request for it, and then uses all of those approvals
// together. Otherwise it records the call under the first policy without one,
// or finds that policy's still-open request, and returns the token with
// instructions. No approval is used up while another policy still holds the
// call.
func (s *Service) HoldCall(ctx context.Context, call HoldInput) (HoldOutcome, error) {
	if len(call.Policies) == 0 {
		return HoldOutcome{Allowed: true, Token: "", UserMessage: "", Policy: nil}, nil
	}
	fingerprint, err := CallFingerprint(call.ServerID, call.ToolName, call.Arguments)
	if err != nil {
		return HoldOutcome{}, fmt.Errorf("fingerprint call: %w", err)
	}

	queries := repo.New(s.db)
	for _, policy := range call.Policies {
		if err := queries.ExpireLapsedOpenApproval(ctx, repo.ExpireLapsedOpenApprovalParams{
			OrganizationID:  call.OrganizationID,
			ProjectID:       call.ProjectID,
			RiskPolicyID:    policy.ID,
			RequesterUrn:    call.RequesterURN,
			ToolName:        call.ToolName,
			CallFingerprint: fingerprint,
		}); err != nil {
			return HoldOutcome{}, fmt.Errorf("expire lapsed approval: %w", err)
		}
	}

	missing, err := s.consumeAll(ctx, call, fingerprint)
	if err != nil {
		return HoldOutcome{}, err
	}
	if missing == nil {
		return HoldOutcome{Allowed: true, Token: "", UserMessage: "", Policy: nil}, nil
	}

	token, err := NewToken()
	if err != nil {
		return HoldOutcome{}, err
	}
	row, err := queries.UpsertBlockedApproval(ctx, repo.UpsertBlockedApprovalParams{
		OrganizationID:   call.OrganizationID,
		ProjectID:        call.ProjectID,
		RiskPolicyID:     missing.ID,
		PolicyVersion:    missing.Version,
		Token:            token,
		RequesterUrn:     call.RequesterURN,
		RequesterUserID:  conv.ToPGTextEmpty(call.RequesterUserID),
		Surface:          call.Surface,
		SourceAdapter:    pgtype.Text{String: "", Valid: false},
		SessionID:        pgtype.Text{String: "", Valid: false},
		ChatID:           conv.StringToNullUUID(call.ChatID),
		ToolsetID:        conv.StringToNullUUID(call.ToolsetID),
		McpServerID:      conv.StringToNullUUID(call.ServerID),
		ServerUrl:        pgtype.Text{String: "", Valid: false},
		ToolName:         call.ToolName,
		CallFingerprint:  fingerprint,
		ArgumentsPreview: argumentsPreview(call.Arguments),
	})
	if err != nil {
		return HoldOutcome{}, fmt.Errorf("record held call: %w", err)
	}
	return HoldOutcome{
		Allowed:     false,
		Token:       row.Token,
		UserMessage: s.holdMessage(missing.Name, call.ToolName, row),
		Policy:      missing,
	}, nil
}

// consumeAll uses one approval per policy in a single transaction. It returns
// the first policy without an approval, in which case nothing is used up.
func (s *Service) consumeAll(ctx context.Context, call HoldInput, fingerprint string) (*policycore.Policy, error) {
	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin approval release: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(dbtx)
	consumedIDs := make([]string, 0, len(call.Policies))
	for i := range call.Policies {
		policy := &call.Policies[i]
		consumed, err := queries.ConsumeApprovedApproval(ctx, repo.ConsumeApprovedApprovalParams{
			OrganizationID:  call.OrganizationID,
			ProjectID:       call.ProjectID,
			RiskPolicyID:    policy.ID,
			RequesterUrn:    call.RequesterURN,
			ToolName:        call.ToolName,
			CallFingerprint: fingerprint,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return policy, nil
		}
		if err != nil {
			return nil, fmt.Errorf("consume approval: %w", err)
		}
		consumedIDs = append(consumedIDs, consumed.ID.String())
	}
	if err := dbtx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit approval release: %w", err)
	}
	for _, id := range consumedIDs {
		s.logger.InfoContext(ctx, "approved call released", attr.SlogRiskPolicyApprovalID(id))
	}
	return nil, nil
}

// RequestInput is an agent's approval request for a held call.
type RequestInput struct {
	// Token is the request token from the block message. It names one held
	// call, and holding it is enough to request approval for that call.
	Token string

	// RequesterURN, when set, is an authenticated caller that must match the
	// principal that made the held call. Empty skips the check: approvers
	// still decide, and only the original caller's retry of the same call can
	// use the approval.
	RequesterURN string

	// OrganizationID, when set with RequesterURN, is the caller's
	// organization.
	OrganizationID string

	// Reason is the agent's one-sentence reason, shown to approvers.
	Reason string

	// Scope is what the agent asks to be approved. Empty means exact_once.
	Scope string

	// ExpiresIn is how long the agent will wait. Zero means the default.
	ExpiresIn time.Duration
}

// Request files a held call's approval request. Repeating it on a request that
// is already pending, or already decided, returns that request unchanged.
func (s *Service) Request(ctx context.Context, input RequestInput) (repo.RiskPolicyApproval, Status, error) {
	if !IsToken(input.Token) {
		return repo.RiskPolicyApproval{}, "", ErrNotFound
	}
	queries := repo.New(s.db)
	row, err := queries.GetApprovalByToken(ctx, input.Token)
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.RiskPolicyApproval{}, "", ErrNotFound
	}
	if err != nil {
		return repo.RiskPolicyApproval{}, "", fmt.Errorf("get approval by token: %w", err)
	}
	if input.RequesterURN != "" && (row.RequesterUrn != input.RequesterURN || row.OrganizationID != input.OrganizationID) {
		return repo.RiskPolicyApproval{}, "", ErrNotFound
	}
	if Status(row.Status) != StatusBlocked {
		return row, s.EffectiveStatus(row), nil
	}

	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return repo.RiskPolicyApproval{}, "", fmt.Errorf("%w: reason is required", ErrInvalidRequest)
	}
	if utf8.RuneCountInString(reason) > maxReasonRunes {
		return repo.RiskPolicyApproval{}, "", fmt.Errorf("%w: reason must be at most %d characters", ErrInvalidRequest, maxReasonRunes)
	}
	scope := input.Scope
	if scope == "" {
		scope = ScopeExactOnce
	}
	if scope != ScopeExactOnce {
		return repo.RiskPolicyApproval{}, "", fmt.Errorf("%w: scope %q is not supported yet; use %s", ErrInvalidRequest, scope, ScopeExactOnce)
	}
	ttl := input.ExpiresIn
	if ttl == 0 {
		ttl = defaultRequestTTL
	}
	if ttl < minRequestTTL || ttl > maxRequestTTL {
		return repo.RiskPolicyApproval{}, "", fmt.Errorf("%w: expires_in must be between %s and %s", ErrInvalidRequest, minRequestTTL, maxRequestTTL)
	}
	requestedScope, err := json.Marshal(scopeDocument{Kind: scope})
	if err != nil {
		return repo.RiskPolicyApproval{}, "", fmt.Errorf("encode requested scope: %w", err)
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return repo.RiskPolicyApproval{}, "", fmt.Errorf("begin approval request: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	updated, err := repo.New(dbtx).MarkApprovalRequested(ctx, repo.MarkApprovalRequestedParams{
		OrganizationID: row.OrganizationID,
		Reason:         conv.ToPGText(reason),
		RequestedScope: requestedScope,
		ExpiresAt:      pgtype.Timestamptz{Time: s.now().Add(ttl), InfinityModifier: pgtype.Finite, Valid: true},
		ProjectID:      row.ProjectID,
		ID:             row.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent request won the race; report its result.
		current, getErr := queries.GetApproval(ctx, repo.GetApprovalParams{OrganizationID: row.OrganizationID, ProjectID: row.ProjectID, ID: row.ID})
		if getErr != nil {
			return repo.RiskPolicyApproval{}, "", fmt.Errorf("reload approval: %w", getErr)
		}
		return current, s.EffectiveStatus(current), nil
	}
	if err != nil {
		return repo.RiskPolicyApproval{}, "", fmt.Errorf("mark approval requested: %w", err)
	}
	policyName, err := s.policyName(ctx, dbtx, updated)
	if err != nil {
		return repo.RiskPolicyApproval{}, "", err
	}
	actor, err := urn.ParsePrincipal(updated.RequesterUrn)
	if err != nil {
		return repo.RiskPolicyApproval{}, "", fmt.Errorf("parse requester: %w", err)
	}
	if err := s.audit.LogRiskPolicyApprovalRequest(ctx, dbtx, audit.LogRiskPolicyApprovalEvent{
		OrganizationID:         updated.OrganizationID,
		ProjectID:              updated.ProjectID,
		Actor:                  actor,
		ActorDisplayName:       nil,
		ActorSlug:              nil,
		RiskPolicyID:           updated.RiskPolicyID,
		RiskPolicyName:         policyName,
		ApprovalSnapshotBefore: auditSnapshot(row),
		ApprovalSnapshotAfter:  auditSnapshot(updated),
	}); err != nil {
		return repo.RiskPolicyApproval{}, "", fmt.Errorf("audit approval request: %w", err)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return repo.RiskPolicyApproval{}, "", fmt.Errorf("commit approval request: %w", err)
	}
	s.logger.InfoContext(ctx, "approval requested", attr.SlogRiskPolicyApprovalID(updated.ID.String()))
	return updated, StatusPending, nil
}

// WatchStatus is the status a watcher sees for a token: approved once an
// approver approved it (and still after the retry used it), otherwise pending,
// blocked, denied or expired.
func (s *Service) WatchStatus(ctx context.Context, token string) (Status, error) {
	if !IsToken(token) {
		return "", ErrNotFound
	}
	row, err := repo.New(s.db).GetApprovalByToken(ctx, token)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get approval by token: %w", err)
	}
	status := s.EffectiveStatus(row)
	if status == StatusConsumed {
		return StatusApproved, nil
	}
	return status, nil
}

// DecideInput is an approver's decision on a pending request.
type DecideInput struct {
	// OrganizationID is the organization the request belongs to.
	OrganizationID string

	// ProjectID is the project the request belongs to.
	ProjectID uuid.UUID

	// ApprovalID is the request being decided.
	ApprovalID uuid.UUID

	// Approve is true to approve and false to deny.
	Approve bool

	// DeciderUserID is the person deciding.
	DeciderUserID string

	// DeciderDisplayName names the person deciding in the audit log, when known.
	DeciderDisplayName *string

	// Via is where the decision came from.
	Via DecidedVia

	// Note is an optional message for the agent.
	Note string
}

// Decide records an approver's decision. The decider must be on the policy's
// approver list, directly or through a role, and must not be the person who
// made the call.
func (s *Service) Decide(ctx context.Context, input DecideInput) (repo.RiskPolicyApproval, error) {
	queries := repo.New(s.db)
	row, err := queries.GetApproval(ctx, repo.GetApprovalParams{OrganizationID: input.OrganizationID, ProjectID: input.ProjectID, ID: input.ApprovalID})
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.RiskPolicyApproval{}, ErrNotFound
	}
	if err != nil {
		return repo.RiskPolicyApproval{}, fmt.Errorf("get approval: %w", err)
	}
	if row.RequesterUserID.Valid && row.RequesterUserID.String == input.DeciderUserID {
		return repo.RiskPolicyApproval{}, ErrSelfApproval
	}

	policyRow, err := riskrepo.New(s.db).GetRiskPolicy(ctx, riskrepo.GetRiskPolicyParams{ID: row.RiskPolicyID, ProjectID: row.ProjectID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && policyRow.OrganizationID != input.OrganizationID) {
		return repo.RiskPolicyApproval{}, ErrNotFound
	}
	if err != nil {
		return repo.RiskPolicyApproval{}, fmt.Errorf("get policy: %w", err)
	}
	if err := s.requireApprover(ctx, input.OrganizationID, input.DeciderUserID, policycore.Project(policyRow, nil).ApprovalConfig); err != nil {
		return repo.RiskPolicyApproval{}, err
	}

	status := StatusDenied
	grantedScope := []byte(nil)
	if input.Approve {
		status = StatusApproved
		grantedScope = row.RequestedScope
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return repo.RiskPolicyApproval{}, fmt.Errorf("begin approval decision: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	decided, err := repo.New(dbtx).DecideApproval(ctx, repo.DecideApprovalParams{
		OrganizationID: input.OrganizationID,
		Status:         string(status),
		DecidedByUrn:   conv.ToPGText(urn.NewPrincipal(urn.PrincipalTypeUser, input.DeciderUserID).String()),
		DecidedVia:     conv.ToPGText(string(input.Via)),
		DecisionNote:   conv.ToPGTextEmpty(strings.TrimSpace(input.Note)),
		GrantedScope:   grantedScope,
		ProjectID:      input.ProjectID,
		ID:             input.ApprovalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.RiskPolicyApproval{}, ErrNotPending
	}
	if err != nil {
		return repo.RiskPolicyApproval{}, fmt.Errorf("decide approval: %w", err)
	}

	logDecision := s.audit.LogRiskPolicyApprovalDeny
	if input.Approve {
		logDecision = s.audit.LogRiskPolicyApprovalApprove
	}
	if err := logDecision(ctx, dbtx, audit.LogRiskPolicyApprovalEvent{
		OrganizationID:         decided.OrganizationID,
		ProjectID:              decided.ProjectID,
		Actor:                  urn.NewPrincipal(urn.PrincipalTypeUser, input.DeciderUserID),
		ActorDisplayName:       input.DeciderDisplayName,
		ActorSlug:              nil,
		RiskPolicyID:           decided.RiskPolicyID,
		RiskPolicyName:         policyRow.Name,
		ApprovalSnapshotBefore: auditSnapshot(row),
		ApprovalSnapshotAfter:  auditSnapshot(decided),
	}); err != nil {
		return repo.RiskPolicyApproval{}, fmt.Errorf("audit approval decision: %w", err)
	}
	if err := dbtx.Commit(ctx); err != nil {
		return repo.RiskPolicyApproval{}, fmt.Errorf("commit approval decision: %w", err)
	}
	s.logger.InfoContext(ctx, "approval decided", attr.SlogRiskPolicyApprovalID(decided.ID.String()), attr.SlogRiskPolicyApprovalStatus(string(status)))
	return decided, nil
}

func (s *Service) requireApprover(ctx context.Context, organizationID, userID string, config *policycore.ApprovalConfig) error {
	principals, err := s.ApproverPrincipals(ctx, organizationID, userID)
	if err != nil {
		return err
	}
	if !CanApprove(principals, config) {
		return ErrNotApprover
	}
	return nil
}

// ApproverPrincipals lists the principals a person can approve as: their user
// and every role they hold in the organization. "All users" is left out, since
// an approver list never names it.
func (s *Service) ApproverPrincipals(ctx context.Context, organizationID, userID string) ([]string, error) {
	if userID == "" {
		return []string{}, nil
	}
	principals, err := authz.ResolveUserPrincipals(ctx, s.db, organizationID, userID)
	if err != nil {
		return nil, fmt.Errorf("resolve approver principals: %w", err)
	}
	out := make([]string, 0, len(principals))
	for _, principal := range principals {
		if principal.ID == urn.AllUsersPrincipalID {
			continue
		}
		out = append(out, principal.String())
	}
	return out, nil
}

// CanApprove reports whether any of principals is on the policy's approver
// list.
func CanApprove(principals []string, config *policycore.ApprovalConfig) bool {
	if config == nil {
		return false
	}
	for _, principal := range principals {
		if slices.Contains(config.Approvers, principal) {
			return true
		}
	}
	return false
}

// EffectiveStatus reports an open request or an unused approval whose window
// has passed as expired, without waiting for a sweep to rewrite the row.
func (s *Service) EffectiveStatus(row repo.RiskPolicyApproval) Status {
	status := Status(row.Status)
	// An approval no retry used before the window closed is as dead as an
	// undecided request: the gateway no longer accepts it.
	lapsable := status == StatusBlocked || status == StatusPending || status == StatusApproved
	if lapsable && row.ExpiresAt.Valid && !row.ExpiresAt.Time.After(s.now()) {
		return StatusExpired
	}
	return status
}

func (s *Service) holdMessage(policyName, toolName string, row repo.RiskPolicyApproval) string {
	if s.EffectiveStatus(row) == StatusPending {
		return fmt.Sprintf(`Waiting: calling %s needs approval under risk policy %q, and approval was already requested.
If you are not already waiting for the decision, wait with this command. It prints one line when the request is decided:
  %s
When it prints APPROVED, retry this exact call with the same arguments. On anything else, do not retry; tell the user.`,
			toolName, policyName, s.waitCommand(row.Token))
	}
	return fmt.Sprintf(`Blocked: calling %s needs approval under risk policy %q.
1. Request approval with this command. Replace the reason with one sentence telling the approver why the call is needed, without single quotes:
   curl -sS -X POST '%s' -H 'Authorization: Bearer %s' -H 'Content-Type: application/json' -d '{"reason":"Why this call is needed."}'
   To wait longer or shorter than 30 minutes, add "expires_in" in seconds (60 to 86400) to the JSON.
2. Its response tells you how to wait for the decision. Do not retry this call before it is approved.`,
		toolName, policyName, s.RequestURL(), row.Token)
}

// scopeDocument is the stored form of a requested or granted scope.
type scopeDocument struct {
	// Kind is the scope kind, such as exact_once.
	Kind string `json:"kind"`
}

// CallFingerprint is the SHA-256 (hex) of the server, the tool and the
// canonical JSON of the arguments. Two calls get the same fingerprint exactly
// when they target the same tool with the same arguments, whatever the key
// order or whitespace.
func CallFingerprint(serverID, toolName string, arguments []byte) (string, error) {
	canonical, err := canonicalJSON(arguments)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	sum.Write([]byte(serverID))
	sum.Write([]byte{0})
	sum.Write([]byte(toolName))
	sum.Write([]byte{0})
	sum.Write(canonical)
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// canonicalJSON re-encodes JSON with sorted object keys and no insignificant
// whitespace. Numbers keep their literal form. Empty input is treated as an
// empty object.
func canonicalJSON(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return []byte("{}"), nil
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode arguments: %w", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode canonical arguments: %w", err)
	}
	return canonical, nil
}

// argumentsPreview keeps a bounded copy of the arguments for approvers: at
// most maxPreviewKeys top-level keys, with long strings and nested values
// truncated. When keys are left out, an omittedArgumentsKey entry says how
// many, because the approval covers every argument. It always returns a JSON
// object.
func argumentsPreview(raw []byte) []byte {
	// Keep numbers as written: approvers must see the exact value the
	// fingerprint covers, not a float rounding of a large id.
	var args map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&args); err != nil || len(args) == 0 {
		return []byte("{}")
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	omitted := max(len(keys)-maxPreviewKeys, 0)
	keys = keys[:len(keys)-omitted]
	preview := make(map[string]any, len(keys)+1)
	for _, key := range keys {
		preview[key] = previewValue(args[key])
	}
	if omitted > 0 {
		preview[unusedKey(preview, omittedArgumentsKey)] = fmt.Sprintf("arguments not shown: %d", omitted)
	}
	encoded, err := json.Marshal(preview)
	if err != nil {
		return []byte("{}")
	}
	return encoded
}

// unusedKey returns key, with "…" appended until no entry of m has it.
func unusedKey(m map[string]any, key string) string {
	for {
		if _, taken := m[key]; !taken {
			return key
		}
		key += "…"
	}
}

func previewValue(value any) any {
	switch v := value.(type) {
	case string:
		return truncateRunes(v, maxPreviewStringRunes)
	case map[string]any, []any:
		encoded, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return truncateRunes(string(encoded), maxPreviewStringRunes)
	default:
		return v
	}
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}
