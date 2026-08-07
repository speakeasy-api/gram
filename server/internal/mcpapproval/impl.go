// Package mcpapproval serves the MCP approval workflow: the queue of servers
// awaiting a decision, the evidence gathered for each, and the durable record
// of what was decided and why.
//
// The evidence sub-packages (identity, capability, authority, packagemeta,
// provenance) derive the signals; this package exposes them and records the
// decision an admin makes on them. Nothing here adjudicates.
package mcpapproval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_approval/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/identity"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// defaultPageLimit bounds a queue page when the caller names no limit.
const defaultPageLimit = 50

// maxPageLimit caps a caller-supplied page size.
const maxPageLimit = 200

// decisionApproved and decisionDenied are the only decisions accepted.
// Validated here rather than with a database CHECK, per the schema
// conventions, so the set can change without a migration.
const (
	decisionApproved = "approved"
	decisionDenied   = "denied"
)

// targetKindServerURL and targetKindStdioCommand are the reference namespaces
// a request may name. Validated here rather than with a database CHECK, per
// the schema conventions.
const (
	targetKindServerURL    = "server_url"
	targetKindStdioCommand = "stdio_command"
)

// statusRequested is the status a raised or reopened request carries.
const statusRequested = "requested"

// statusFor maps a decision onto the status its request moves to.
var statusFor = map[string]string{
	decisionApproved: "approved",
	decisionDenied:   "denied",
}

type Service struct {
	tracer   trace.Tracer
	logger   *slog.Logger
	db       *pgxpool.Pool
	auth     *auth.Auth
	authz    *authz.Engine
	features *productfeatures.Client
	audit    *audit.Logger
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine, features *productfeatures.Client, auditLogger *audit.Logger) *Service {
	logger = logger.With(attr.SlogComponent("mcpapproval"))

	return &Service{
		tracer:   tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpapproval"),
		logger:   logger,
		db:       db,
		auth:     auth.New(logger, db, sessions, authzEngine),
		authz:    authzEngine,
		features: features,
		audit:    auditLogger,
	}
}

func Attach(mux goahttp.Muxer, service *Service) {
	endpoints := gen.NewEndpoints(service)
	endpoints.Use(middleware.MapErrors())
	endpoints.Use(middleware.TraceMethods(service.tracer))
	srv.Mount(
		mux,
		srv.New(endpoints, mux, goahttp.RequestDecoder, goahttp.ResponseEncoder, nil, nil),
	)
}

func (s *Service) APIKeyAuth(ctx context.Context, key string, schema *security.APIKeyScheme) (context.Context, error) {
	return s.auth.Authorize(ctx, key, schema)
}

// project resolves the caller's project and enforces scope.
//
// Every read and write in this service goes through here, so no handler can
// reach the database without a project id that the server derived and a scope
// the caller actually holds.
func (s *Service) project(ctx context.Context, scope authz.Scope) (uuid.UUID, string, error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil {
		return uuid.Nil, "", oops.C(oops.CodeUnauthorized)
	}

	if err := s.authz.Require(ctx, authz.Check{
		Scope:        scope,
		ResourceKind: "",
		ResourceID:   authCtx.ProjectID.String(),
		Dimensions:   nil,
	}); err != nil {
		return uuid.Nil, "", fmt.Errorf("authorize mcp approval access: %w", err)
	}

	// The product-feature gate is independent of the RBAC check: a grant says
	// who may use the surface, the feature says whether the organization has
	// it at all, and holding the first must not bypass the second. RBAC runs
	// first so an unauthorized caller costs no feature-store work and a
	// feature lookup failure never masks a denial.
	enabled, err := s.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureMCPApproval)
	if err != nil {
		return uuid.Nil, "", oops.E(oops.CodeUnexpected, err, "check mcp approval feature").LogError(ctx, s.logger)
	}
	if !enabled {
		return uuid.Nil, "", oops.E(oops.CodeForbidden, nil, "MCP approval is not enabled for this organization")
	}

	return *authCtx.ProjectID, authCtx.ActiveOrganizationID, nil
}

// member resolves the caller's project and enforces the feature gate without
// demanding a scope. Raising a request deliberately carries no RBAC grant:
// the people asking typically cannot reach the dashboard, and a scope for it
// would either be ungranted for everyone who needs it or granted so
// universally it means nothing — the same posture as the block and bypass
// surfaces. Authentication and project membership still apply, and the
// product-feature gate holds either way.
func (s *Service) member(ctx context.Context) (uuid.UUID, *contextvalues.AuthContext, error) {
	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.ProjectID == nil || authCtx.UserID == "" {
		return uuid.Nil, nil, oops.C(oops.CodeUnauthorized)
	}

	enabled, err := s.features.IsFeatureEnabled(ctx, authCtx.ActiveOrganizationID, productfeatures.FeatureMCPApproval)
	if err != nil {
		return uuid.Nil, nil, oops.E(oops.CodeUnexpected, err, "check mcp approval feature").LogError(ctx, s.logger)
	}
	if !enabled {
		return uuid.Nil, nil, oops.E(oops.CodeForbidden, nil, "MCP approval is not enabled for this organization")
	}

	return *authCtx.ProjectID, authCtx, nil
}

// admission is one ask for a server, ready to be written.
type admission struct {
	targetKind string
	targetRaw  string
	targetKey  string

	// bypassRequestID links the promotion source, when there is one.
	bypassRequestID uuid.NullUUID

	// requesterID and requesterEmail identify who asked. An empty requesterID
	// records the request without a requester row — a block hook cannot
	// always resolve a user, and losing the ask entirely would be worse than
	// losing its attribution.
	requesterID    string
	requesterEmail *string
	note           *string

	// actor is who performed this API call, which for a promotion is the
	// admin rather than the original requester.
	actor string

	// actorEmail is the actor's email, when known.
	actorEmail *string
}

// admit records one ask: the request row is created or reopened, the
// requester is attached, and the create is audited — atomically.
func (s *Service) admit(ctx context.Context, projectID uuid.UUID, organizationID string, adm admission) (*gen.ApprovalRequestSummary, error) {
	resolved := identity.Resolve(adm.targetRaw)

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording approval request").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(s.db).WithTx(dbtx)

	request, err := queries.UpsertApprovalRequest(ctx, repo.UpsertApprovalRequestParams{
		OrganizationID:            organizationID,
		ProjectID:                 projectID,
		TargetKind:                adm.targetKind,
		TargetRaw:                 adm.targetRaw,
		TargetKey:                 adm.targetKey,
		ArtifactRef:               conv.ToPGTextEmpty(resolved.ArtifactRef),
		VersionPinned:             resolved.VersionPinned,
		Status:                    statusRequested,
		RiskPolicyBypassRequestID: adm.bypassRequestID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording approval request").LogError(ctx, s.logger)
	}

	if adm.requesterID != "" {
		if _, err := queries.UpsertApprovalRequestRequester(ctx, repo.UpsertApprovalRequestRequesterParams{
			OrganizationID:       organizationID,
			ProjectID:            projectID,
			McpApprovalRequestID: request.ID,
			UserID:               adm.requesterID,
			UserEmail:            conv.PtrToPGTextEmpty(adm.requesterEmail),
			Note:                 conv.PtrToPGTextEmpty(adm.note),
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error recording requester").LogError(ctx, s.logger)
		}
	}

	if err := s.audit.LogMCPApprovalRequestCreate(ctx, dbtx, audit.LogMCPApprovalRequestCreateEvent{
		OrganizationID:   organizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, adm.actor),
		ActorDisplayName: adm.actorEmail,
		ActorSlug:        nil,
		RequestURN:       urn.NewMCPApprovalRequest(request.ID),
		TargetRaw:        adm.targetRaw,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error auditing approval request").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording approval request").LogError(ctx, s.logger)
	}

	// Re-read for the response so the summary carries the requester count the
	// write just changed.
	row, err := repo.New(s.db).GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: request.ID, ProjectID: projectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading approval request").LogError(ctx, s.logger)
	}

	return summaryView(fromGetRow(row)), nil
}

func (s *Service) CreateRequest(ctx context.Context, payload *gen.CreateRequestPayload) (*gen.ApprovalRequestSummary, error) {
	projectID, authCtx, err := s.member(ctx)
	if err != nil {
		return nil, err
	}

	raw := strings.TrimSpace(payload.Target)
	if raw == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "a server reference is required")
	}

	var key string
	switch payload.TargetKind {
	case targetKindServerURL:
		canonicalKey, display, err := admittableServerURL(raw)
		if err != nil {
			return nil, err
		}
		key = canonicalKey
		// The stored reference is the redacted form: a token pasted into a
		// request URL must not reach every reader of the queue or the audit
		// feed, and the readable scheme, host, and path are what identify
		// the server anyway.
		raw = display
	case targetKindStdioCommand:
		// Collapsed whitespace, so cosmetic spacing differences do not split
		// one server into two reviews.
		key = strings.Join(strings.Fields(raw), " ")
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "target_kind must be server_url or stdio_command")
	}

	// The justification is the one input no automated evidence supplies, so
	// a proactive ask cannot omit it.
	trimmedNote := strings.TrimSpace(payload.Note)
	if trimmedNote == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "a justification is required")
	}
	note := &trimmedNote

	return s.admit(ctx, projectID, authCtx.ActiveOrganizationID, admission{
		targetKind:      payload.TargetKind,
		targetRaw:       raw,
		targetKey:       key,
		bypassRequestID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		requesterID:     authCtx.UserID,
		requesterEmail:  authCtx.Email,
		note:            note,
		actor:           authCtx.UserID,
		actorEmail:      authCtx.Email,
	})
}

func (s *Service) Promote(ctx context.Context, payload *gen.PromotePayload) (*gen.ApprovalRequestSummary, error) {
	projectID, _, err := s.project(ctx, authz.ScopeMCPApprovalDecide)
	if err != nil {
		return nil, err
	}

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	bypassID, err := uuid.Parse(payload.RiskPolicyBypassRequestID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid bypass request id")
	}

	// Resolved under the caller's project, never by id alone. The id arrives
	// from the caller, and there is no database-level pin for this pair (see
	// AIS-470), so this read is the primary control against promoting another
	// project's bypass request into this project's queue.
	bypass, err := repo.New(s.db).GetBypassRequestForPromotion(ctx, repo.GetBypassRequestForPromotionParams{
		ID:        bypassID,
		ProjectID: projectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "bypass request not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading bypass request").LogError(ctx, s.logger)
	}

	// Only a bypass request that names a server can become a server review. A
	// whole-policy bypass names no server to gather evidence about.
	serverURL := bypassServerURL(bypass)
	if serverURL == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "bypass request names no server")
	}

	key, display, err := admittableServerURL(serverURL)
	if err != nil {
		return nil, err
	}

	return s.admit(ctx, projectID, bypass.OrganizationID, admission{
		targetKind:      targetKindServerURL,
		targetRaw:       display,
		targetKey:       key,
		bypassRequestID: uuid.NullUUID{UUID: bypass.ID, Valid: true},
		requesterID:     bypass.RequesterUserID,
		requesterEmail:  conv.FromPGText[string](bypass.RequesterEmail),
		note:            conv.FromPGText[string](bypass.Note),
		actor:           authCtx.UserID,
		actorEmail:      authCtx.Email,
	})
}

// admittableServerURL validates a server URL reference for intake and returns
// the canonical dedupe key plus the redacted form safe to persist and show.
//
// Only http and https are admitted: the MCP backend can reach nothing else,
// and a review for an unreachable reference wastes an admin's attention. The
// key comes from the same canonicalization the shadow-MCP inventory uses, so
// a request, a block, and the org's own traffic converge on one key per
// server.
func admittableServerURL(raw string) (key string, display string, err error) {
	parsed, parseErr := url.Parse(strings.TrimSpace(raw))
	if parseErr != nil {
		return "", "", oops.E(oops.CodeBadRequest, parseErr, "target is not a valid server URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", oops.E(oops.CodeBadRequest, nil, "target must be an http or https URL")
	}

	inventoryURL, ok := shadowmcp.CanonicalizeInventoryURL(raw)
	if !ok {
		return "", "", oops.E(oops.CodeBadRequest, nil, "target is not a valid server URL")
	}

	display, ok = identity.RedactServerURL(raw)
	if !ok {
		return "", "", oops.E(oops.CodeBadRequest, nil, "target is not a valid server URL")
	}

	return inventoryURL.CanonicalURL, display, nil
}

// bypassServerURL extracts the server a bypass request was raised about.
func bypassServerURL(bypass repo.GetBypassRequestForPromotionRow) string {
	var dimensions map[string]string
	if err := json.Unmarshal(bypass.TargetDimensions, &dimensions); err == nil {
		if serverURL := strings.TrimSpace(dimensions[authz.SelectorKeyServerURL]); serverURL != "" {
			return serverURL
		}
	}

	return strings.TrimSpace(conv.FromPGTextOrEmpty[string](bypass.TargetKey))
}

func (s *Service) ListRequests(ctx context.Context, payload *gen.ListRequestsPayload) (*gen.ListApprovalRequestsResult, error) {
	projectID, _, err := s.project(ctx, authz.ScopeMCPApprovalRead)
	if err != nil {
		return nil, err
	}

	limit := int32(defaultPageLimit)
	if payload.Limit != nil && *payload.Limit > 0 {
		limit = min(*payload.Limit, maxPageLimit)
	}

	rows, err := repo.New(s.db).ListApprovalRequests(ctx, repo.ListApprovalRequestsParams{
		ProjectID: projectID,
		Status:    pgText(payload.Status),
		PageLimit: limit,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error listing approval requests").LogError(ctx, s.logger)
	}

	requests := make([]*gen.ApprovalRequestSummary, 0, len(rows))
	for _, row := range rows {
		requests = append(requests, summaryView(fromListRow(row)))
	}

	return &gen.ListApprovalRequestsResult{NextCursor: nil, Requests: requests}, nil
}

func (s *Service) GetRequest(ctx context.Context, payload *gen.GetRequestPayload) (*gen.ApprovalRequestDetail, error) {
	projectID, _, err := s.project(ctx, authz.ScopeMCPApprovalRead)
	if err != nil {
		return nil, err
	}

	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid approval request id")
	}

	queries := repo.New(s.db)

	// Resolved with the project id in the predicate, so a caller who learns an
	// id from a dashboard URL cannot read another tenant's request.
	row, err := queries.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "approval request not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading approval request").LogError(ctx, s.logger)
	}

	requesterRows, err := queries.ListRequestersForApprovalRequest(ctx, repo.ListRequestersForApprovalRequestParams{
		McpApprovalRequestID: requestID,
		ProjectID:            projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading approval requesters").LogError(ctx, s.logger)
	}

	decisionRows, err := queries.ListDecisionsForApprovalRequest(ctx, repo.ListDecisionsForApprovalRequestParams{
		McpApprovalRequestID: requestID,
		ProjectID:            projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading approval decisions").LogError(ctx, s.logger)
	}

	reportRows, err := queries.ListResearchReportsForApprovalRequest(ctx, repo.ListResearchReportsForApprovalRequestParams{
		McpApprovalRequestID: requestID,
		ProjectID:            projectID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading research reports").LogError(ctx, s.logger)
	}

	requesters := make([]*gen.ApprovalRequester, 0, len(requesterRows))
	for _, requester := range requesterRows {
		requesters = append(requesters, &gen.ApprovalRequester{
			UserID:      requester.UserID,
			UserEmail:   fromPGText(requester.UserEmail),
			Note:        fromPGText(requester.Note),
			RequestedAt: requester.RequestedAt.Time.Format(timeFormat),
		})
	}

	decisions := make([]*gen.ApprovalDecision, 0, len(decisionRows))
	for _, decision := range decisionRows {
		decisions = append(decisions, decisionView(decision))
	}

	reports := make([]*gen.ResearchReport, 0, len(reportRows))
	for _, report := range reportRows {
		reports = append(reports, researchReportView(report))
	}

	return &gen.ApprovalRequestDetail{
		Request:             summaryView(fromGetRow(row)),
		Requesters:          requesters,
		Evidence:            rawEvidence(row.CurrentEvidence),
		EvidenceVersion:     evidenceVersion(row.EvidenceVersion),
		EvidenceCollectedAt: optionalTime(row.EvidenceCollectedAt),
		Decisions:           decisions,
		ResearchReports:     reports,
	}, nil
}

func (s *Service) RecordDecision(ctx context.Context, payload *gen.RecordDecisionPayload) (*gen.ApprovalDecision, error) {
	projectID, _, err := s.project(ctx, authz.ScopeMCPApprovalDecide)
	if err != nil {
		return nil, err
	}

	if payload.Decision != decisionApproved && payload.Decision != decisionDenied {
		return nil, oops.E(oops.CodeBadRequest, nil, "decision must be approved or denied")
	}

	// The rationale is the artifact cited when explaining the decision to the
	// requester, so a blank one is rejected rather than recorded.
	rationale := strings.TrimSpace(payload.Rationale)
	if rationale == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "a rationale is required")
	}

	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid approval request id")
	}

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	// Parsed before any database work, so a malformed id costs no
	// transaction and never locks the request row.
	var citedReportID uuid.NullUUID
	if payload.ResearchReportID != nil {
		reportID, err := uuid.Parse(*payload.ResearchReportID)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid research report id")
		}
		citedReportID = uuid.NullUUID{UUID: reportID, Valid: true}
	}

	dbtx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording decision").LogError(ctx, s.logger)
	}
	defer o11y.NoLogDefer(func() error { return dbtx.Rollback(ctx) })

	queries := repo.New(s.db).WithTx(dbtx)

	// Read the request under the project id before writing anything. The
	// predicate on the insert would scope it too, but resolving ownership
	// explicitly is what stops a forgotten predicate becoming a tenancy
	// crossing — the failure mode behind AIS-424. The read locks the row so
	// concurrent decisions serialise: the request's status always ends up
	// matching the newest decision rather than whichever transaction happened
	// to commit last.
	request, err := queries.GetApprovalRequestForDecision(ctx, repo.GetApprovalRequestForDecisionParams{ID: requestID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "approval request not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading approval request").LogError(ctx, s.logger)
	}

	// A cited report is resolved against the request being decided and the
	// caller's project before it is written, so a decision can never
	// attribute research about one server to another.
	if citedReportID.Valid {
		if _, err := queries.GetResearchReportForDecision(ctx, repo.GetResearchReportForDecisionParams{
			ID:                   citedReportID.UUID,
			McpApprovalRequestID: requestID,
			ProjectID:            projectID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, oops.E(oops.CodeBadRequest, nil, "research report does not belong to this request")
			}
			return nil, oops.E(oops.CodeUnexpected, err, "error reading research report").LogError(ctx, s.logger)
		}
	}

	granted := payload.GrantedPrincipalUrns
	if payload.Decision == decisionDenied {
		// A denial grants nobody anything, whatever the caller sent.
		granted = nil
	}
	if granted == nil {
		granted = []string{}
	}

	// The evidence is frozen as it stood on the request, and its version is
	// copied rather than defaulted, so a later re-gather cannot rewrite what
	// this reviewer actually saw.
	// The organisation is taken from the request that was just resolved under
	// this project, not from the auth context. The composite foreign key pins
	// a decision to its request's project but not to its organisation, so
	// deriving it here is what stops the two ever disagreeing.
	decision, err := queries.CreateApprovalDecision(ctx, repo.CreateApprovalDecisionParams{
		OrganizationID:       request.OrganizationID,
		ProjectID:            projectID,
		McpApprovalRequestID: requestID,
		Decision:             payload.Decision,
		DecidedBy:            authCtx.UserID,
		Rationale:            pgText(&rationale),
		EvidenceSnapshot:     request.CurrentEvidence,
		EvidenceVersion:      request.EvidenceVersion,
		GrantedPrincipalUrns: granted,
		McpResearchReportID:  citedReportID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording decision").LogError(ctx, s.logger)
	}

	if err := s.audit.LogMCPApprovalRequestDecide(ctx, dbtx, audit.LogMCPApprovalRequestDecideEvent{
		OrganizationID:   request.OrganizationID,
		ProjectID:        projectID,
		Actor:            urn.NewPrincipal(urn.PrincipalTypeUser, authCtx.UserID),
		ActorDisplayName: authCtx.Email,
		ActorSlug:        nil,
		RequestURN:       urn.NewMCPApprovalRequest(requestID),
		Approved:         payload.Decision == decisionApproved,
		TargetRaw:        request.TargetRaw,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error auditing decision").LogError(ctx, s.logger)
	}

	if err := queries.SetApprovalRequestStatus(ctx, repo.SetApprovalRequestStatusParams{
		ID:        requestID,
		ProjectID: projectID,
		Status:    statusFor[payload.Decision],
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error updating approval request status").LogError(ctx, s.logger)
	}

	if err := dbtx.Commit(ctx); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording decision").LogError(ctx, s.logger)
	}

	return decisionView(decision), nil
}
