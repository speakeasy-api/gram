package mcpapproval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/trace"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidence"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// StaffReviews runs ordinary MCP reviews inside one Speakeasy-owned project on
// behalf of verified staff. It adds no review logic of its own: dossiers,
// research runs, spend, telemetry and audit entries all follow the home
// project, so reviewing a server never touches the account it was found in.
type StaffReviews struct {
	service        *Service
	organizationID string
	projectID      uuid.UUID
}

// StaffReview is one server's dossier and its latest research run.
type StaffReview struct {
	// RequestID identifies the review in the home project.
	RequestID string `json:"request_id"`
	// Evidence is untrusted, server-authored data about the reviewed server.
	Evidence json.RawMessage `json:"evidence,omitempty"`
	// EvidenceCollectedAt timestamps the evidence, when it has been gathered.
	EvidenceCollectedAt string `json:"evidence_collected_at,omitempty"`
	// Research is the most recent research run, when one was started.
	Research *StaffResearch `json:"research,omitempty"`
}

// StaffResearch summarizes one research run.
type StaffResearch struct {
	// Status is running, completed, or failed.
	Status string `json:"status"`
	// Report holds the cited findings once the run completes.
	Report json.RawMessage `json:"report,omitempty"`
	// Error says why the run failed, when it did.
	Error string `json:"error,omitempty"`
	// StartedAt timestamps the run.
	StartedAt string `json:"started_at,omitempty"`
	// CompletedAt timestamps its end.
	CompletedAt string `json:"completed_at,omitempty"`
}

// ErrStaffReviewDisabled reports that the home project's rollout gates refuse the review.
var ErrStaffReviewDisabled = errors.New("MCP review is not enabled for the staff review organization")

// NewStaffReviews builds the staff entry point. The returned service never
// serves HTTP, so it carries none of the session or RBAC dependencies the
// customer-facing surface needs.
func NewStaffReviews(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, flags feature.Provider, auditLogger *audit.Logger, assembler *evidence.Assembler, research ResearchStarter, organizationID string, projectID uuid.UUID) *StaffReviews {
	logger = logger.With(attr.SlogComponent("mcpapproval"))
	return &StaffReviews{
		service: &Service{
			tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpapproval"),
			logger:     logger,
			db:         db,
			auth:       nil,
			authz:      nil,
			flags:      flags,
			audit:      auditLogger,
			evidence:   assembler,
			research:   research,
			gapRetryMu: sync.Mutex{},
			gapRetryAt: make(map[uuid.UUID]time.Time),
		},
		organizationID: organizationID,
		projectID:      projectID,
	}
}

// Available reports whether the home organization's rollout gates admit a new
// review, so staff surfaces can say so before anyone tries to start one.
func (r *StaffReviews) Available(ctx context.Context) bool {
	if r.service.research == nil {
		return false
	}
	return r.service.requireFeature(ctx, r.organizationID) == nil && r.service.requireResearchEnabled(ctx, r.organizationID) == nil
}

// Find returns the existing review for a server URL, or nil when none exists.
// It is a plain read: no evidence is gathered and nothing is recorded. It skips
// the rollout gates on purpose, because they stop new spend and must not hide
// findings staff already paid for, including while the kill switch is on.
func (r *StaffReviews) Find(ctx context.Context, serverURL string) (*StaffReview, error) {
	key, _, err := admittableServerURL(serverURL)
	if err != nil {
		return nil, fmt.Errorf("resolve staff review target: %w", err)
	}
	row, err := repo.New(r.service.db).GetApprovalRequestByTarget(ctx, repo.GetApprovalRequestByTargetParams{ProjectID: r.projectID, TargetKind: targetKindServerURL, TargetKey: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read staff review: %w", err)
	}
	return r.review(ctx, row.ID)
}

// Start opens the server's dossier when needed, then starts a research run. A
// run already in flight is returned rather than paid for twice.
func (r *StaffReviews) Start(ctx context.Context, serverURL, actor string, actorEmail *string) (*StaffReview, error) {
	if strings.TrimSpace(actor) == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	if _, _, err := admittableSecureServerURL(serverURL); err != nil {
		return nil, fmt.Errorf("check staff review target: %w", err)
	}
	if err := r.service.requireFeature(ctx, r.organizationID); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStaffReviewDisabled, err)
	}
	if err := r.service.requireResearchEnabled(ctx, r.organizationID); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrStaffReviewDisabled, err)
	}
	if r.service.research == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "the research runner is not available")
	}
	summary, err := r.service.ensureServerReview(ctx, r.projectID, r.organizationID, actor, actorEmail, serverURL)
	if err != nil {
		return nil, fmt.Errorf("open staff review: %w", err)
	}
	requestID, err := uuid.Parse(summary.ID)
	if err != nil {
		return nil, fmt.Errorf("parse staff review id: %w", err)
	}
	if _, err := r.service.startResearch(ctx, r.projectID, r.organizationID, requestID, actor, actorEmail); err != nil {
		return nil, fmt.Errorf("start staff research: %w", err)
	}
	return r.review(ctx, requestID)
}

func (r *StaffReviews) review(ctx context.Context, requestID uuid.UUID) (*StaffReview, error) {
	detail, err := r.service.requestDetail(ctx, r.projectID, requestID)
	if err != nil {
		return nil, fmt.Errorf("read staff review detail: %w", err)
	}
	result := &StaffReview{RequestID: detail.Request.ID, Evidence: nil, EvidenceCollectedAt: valueOrEmpty(detail.EvidenceCollectedAt), Research: nil}
	if detail.EvidenceCollectedAt != nil && detail.Evidence != nil {
		result.Evidence, err = withoutExposure(detail.Evidence, detail.EvidenceVersion)
		if err != nil {
			return nil, err
		}
	}
	if len(detail.ResearchReports) > 0 {
		latest := detail.ResearchReports[0]
		report, err := json.Marshal(latest.Report)
		if err != nil {
			return nil, fmt.Errorf("encode staff research report: %w", err)
		}
		result.Research = &StaffResearch{Status: latest.Status, Report: report, Error: valueOrEmpty(latest.Error), StartedAt: valueOrEmpty(latest.StartedAt), CompletedAt: valueOrEmpty(latest.CompletedAt)}
	}
	return result, nil
}

// withoutExposure removes the traffic section. The dossier lives in the home
// project, so that section describes Speakeasy's own traffic and would read as
// a statement about the account the server was found in.
func withoutExposure(stored any, version *int) (json.RawMessage, error) {
	raw, err := json.Marshal(stored)
	if err != nil {
		return nil, fmt.Errorf("encode staff review evidence: %w", err)
	}
	currentVersion := evidence.Version
	if version != nil {
		currentVersion = *version
	}
	document, err := evidence.DecodeDocument(raw, currentVersion)
	if err != nil {
		return nil, fmt.Errorf("decode staff review evidence: %w", err)
	}
	document.Exposure = nil
	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode staff review evidence: %w", err)
	}
	return encoded, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
