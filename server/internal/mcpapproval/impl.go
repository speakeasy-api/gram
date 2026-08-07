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
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"
	goahttp "goa.design/goa/v3/http"
	"goa.design/goa/v3/security"

	srv "github.com/speakeasy-api/gram/server/gen/http/mcp_approval/server"
	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/auth"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
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

// statusFor maps a decision onto the status its request moves to.
var statusFor = map[string]string{
	decisionApproved: "approved",
	decisionDenied:   "denied",
}

type Service struct {
	tracer trace.Tracer
	logger *slog.Logger
	db     *pgxpool.Pool
	auth   *auth.Auth
	authz  *authz.Engine
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine) *Service {
	logger = logger.With(attr.SlogComponent("mcpapproval"))

	return &Service{
		tracer: tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpapproval"),
		logger: logger,
		db:     db,
		auth:   auth.New(logger, db, sessions, authzEngine),
		authz:  authzEngine,
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

	return *authCtx.ProjectID, authCtx.ActiveOrganizationID, nil
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

	return &gen.ApprovalRequestDetail{
		Request:             summaryView(fromGetRow(row)),
		Requesters:          requesters,
		Evidence:            rawEvidence(row.CurrentEvidence),
		EvidenceVersion:     evidenceVersion(row.EvidenceVersion),
		EvidenceCollectedAt: optionalTime(row.EvidenceCollectedAt),
		Decisions:           decisions,
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

	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid approval request id")
	}

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
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
	// crossing — the failure mode behind AIS-424.
	request, err := queries.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "approval request not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading approval request").LogError(ctx, s.logger)
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
		Rationale:            pgText(payload.Rationale),
		EvidenceSnapshot:     request.CurrentEvidence,
		EvidenceVersion:      request.EvidenceVersion,
		GrantedPrincipalUrns: granted,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording decision").LogError(ctx, s.logger)
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
