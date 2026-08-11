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
	"sync"
	"time"

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
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidence"
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

// maxTargetLength and maxNoteLength bound the free-text intake fields. They
// mirror the MaxLength bounds in the service design, enforced here as well so
// a caller reaching the service without the generated transport validation
// gets the same answer.
const (
	maxTargetLength = 2048
	maxNoteLength   = 4000
)

// targetKindServerURL and targetKindStdioCommand are the reference namespaces
// a request may name. Validated here rather than with a database CHECK, per
// the schema conventions.
const (
	targetKindServerURL    = "server_url"
	targetKindStdioCommand = "stdio_command"
)

// decisionApproved and decisionDenied are the only decisions accepted.
// Validated here rather than with a database CHECK, per the schema
// conventions, so the set can change without a migration.
const (
	decisionApproved = "approved"
	decisionDenied   = "denied"
)

// statusRequested is the status a raised or reopened request carries.
const statusRequested = "requested"

// statusUnreviewed marks an evidence dossier nobody has asked about: opened
// so a server can be inspected, it stays out of the decision queue and
// upgrades in place to requested the moment someone actually asks.
const statusUnreviewed = "unreviewed"

// gatherTimeout is the overall backstop for evidence gathering at intake.
// Each source inside the assembler carries its own tighter deadline, so one
// unreachable registry costs its own budget and lands in the document's gaps
// rather than holding the admission for this whole window; this bound only
// matters if every source is slow at once. It is sized above the sum of the
// sequential per-source budgets (a remote target consults four sources at 3s
// each), so a gather where every earlier source times out still reaches the
// catalog lookup instead of losing it to the backstop.
const gatherTimeout = 14 * time.Second

// gapRetryCooldown bounds how often one gapped dossier re-attempts its gather
// from the read path. The retry exists so an outage-time dossier heals on a
// later view; without a floor, a persistent source outage would turn every
// page view into another full gather — up to gatherTimeout of user-visible
// latency apiece, aimed at sources that are already struggling. The cooldown
// is tracked in memory per replica: its job is damping, not cross-replica
// coordination, and a handful of replicas each retrying once a minute is
// still a bounded trickle.
const gapRetryCooldown = time.Minute

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
	evidence *evidence.Assembler

	// gapRetryMu guards gapRetryAt.
	gapRetryMu sync.Mutex

	// gapRetryAt records when each gapped dossier last re-attempted its
	// gather from the read path, enforcing gapRetryCooldown.
	gapRetryAt map[uuid.UUID]time.Time
}

var (
	_ gen.Service = (*Service)(nil)
	_ gen.Auther  = (*Service)(nil)
)

func NewService(logger *slog.Logger, tracerProvider trace.TracerProvider, db *pgxpool.Pool, sessions *sessions.Manager, authzEngine *authz.Engine, features *productfeatures.Client, auditLogger *audit.Logger, assembler *evidence.Assembler) *Service {
	logger = logger.With(attr.SlogComponent("mcpapproval"))

	return &Service{
		tracer:     tracerProvider.Tracer("github.com/speakeasy-api/gram/server/internal/mcpapproval"),
		logger:     logger,
		db:         db,
		auth:       auth.New(logger, db, sessions, authzEngine),
		authz:      authzEngine,
		features:   features,
		audit:      auditLogger,
		evidence:   assembler,
		gapRetryMu: sync.Mutex{},
		gapRetryAt: make(map[uuid.UUID]time.Time),
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
	if err := s.requireFeature(ctx, authCtx.ActiveOrganizationID); err != nil {
		return uuid.Nil, "", err
	}

	return *authCtx.ProjectID, authCtx.ActiveOrganizationID, nil
}

// requireFeature enforces the organization-level product gate every entry
// point shares, whether or not that entry point also demands a scope.
func (s *Service) requireFeature(ctx context.Context, organizationID string) error {
	enabled, err := s.features.IsFeatureEnabled(ctx, organizationID, productfeatures.FeatureMCPApproval)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "check mcp approval feature").LogError(ctx, s.logger)
	}
	if !enabled {
		return oops.E(oops.CodeForbidden, nil, "MCP approval is not enabled for this organization")
	}

	return nil
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

	if err := s.requireFeature(ctx, authCtx.ActiveOrganizationID); err != nil {
		return uuid.Nil, nil, err
	}

	return *authCtx.ProjectID, authCtx, nil
}

// admission is one ask for a server, ready to be written.
type admission struct {
	targetKind string
	targetRaw  string
	targetKey  string

	// status the row is admitted with: statusRequested for a real ask,
	// statusUnreviewed for an evidence dossier opened without one. The upsert
	// only ever upgrades unreviewed or denied rows toward requested, never
	// the other way.
	status string

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

	// Evidence is gathered before the transaction — it does network and
	// ClickHouse I/O that must not run under a database transaction — and it
	// is best-effort: an admission is never lost to a flaky source, and the
	// per-source failures are recorded inside the document itself as gaps.
	gatherCtx, cancelGather := context.WithTimeout(ctx, gatherTimeout)
	defer cancelGather()
	document, gatherErr := s.evidence.Assemble(gatherCtx, projectID, resolved)
	if gatherErr != nil {
		s.logger.ErrorContext(ctx, "failed to assemble approval evidence", attr.SlogError(gatherErr))
	}

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
		Status:                    adm.status,
		RiskPolicyBypassRequestID: adm.bypassRequestID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error recording approval request").LogError(ctx, s.logger)
	}

	if gatherErr == nil {
		if err := queries.SetApprovalRequestEvidence(ctx, repo.SetApprovalRequestEvidenceParams{
			CurrentEvidence: document,
			EvidenceVersion: evidence.Version,
			ID:              request.ID,
			ProjectID:       projectID,
		}); err != nil {
			return nil, oops.E(oops.CodeUnexpected, err, "error storing evidence").LogError(ctx, s.logger)
		}
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

	// A real ask audits every time — a repeat ask is accumulating demand the
	// feed should show. An unreviewed dossier audits only when the upsert
	// actually inserted the row: concurrent first opens of the same server
	// page, or a retry after a failed gather, land on an existing row and must
	// not audit a create that did not happen.
	if adm.status != statusUnreviewed || request.Inserted {
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

// EnsureServerReview resolves the evidence dossier for a server URL, opening
// one when none exists. It records no ask and no decision: the row is written
// as unreviewed, stays out of the queue, and upgrades in place when someone
// actually requests the server. Reading evidence must never require deciding
// first, so the server page calls this for any URL it shows.
func (s *Service) EnsureServerReview(ctx context.Context, payload *gen.EnsureServerReviewPayload) (*gen.ApprovalRequestSummary, error) {
	projectID, organizationID, err := s.project(ctx, authz.ScopeMCPApprovalRead)
	if err != nil {
		return nil, err
	}

	authCtx, _ := contextvalues.GetAuthContext(ctx)
	if authCtx == nil || authCtx.UserID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	trimmedTarget := strings.TrimSpace(payload.Target)
	if len(trimmedTarget) > maxTargetLength {
		return nil, oops.E(oops.CodeBadRequest, nil, "target must be at most %d characters", maxTargetLength)
	}

	key, display, err := admittableServerURL(trimmedTarget)
	if err != nil {
		return nil, err
	}

	// A dossier that already exists with a complete gathered document
	// resolves as a plain read: the server page calls this endpoint on every
	// view, so a repeat resolve must not re-run the evidence probes, touch
	// the row, or audit a create that did not happen. Two failure shapes
	// retry instead. A row with no evidence at all — the whole gather
	// errored, or a concurrent first open won the insert — falls through to
	// admit; admit audits a create only when the upsert actually inserted,
	// so the loser refreshes the same dossier without a duplicate audit
	// entry. A row whose stored document recorded source gaps — an
	// unreachable registry or a failed traffic lookup land in Gaps with
	// evidence_collected_at still set — re-gathers in place, so an
	// outage-time dossier does not keep its gaps forever.
	existing, err := repo.New(s.db).GetApprovalRequestByTarget(ctx, repo.GetApprovalRequestByTargetParams{
		ProjectID:  projectID,
		TargetKind: targetKindServerURL,
		TargetKey:  key,
	})
	switch {
	case err == nil && existing.EvidenceCollectedAt.Valid:
		if !storedEvidenceHasGaps(existing.CurrentEvidence, existing.EvidenceVersion) {
			return summaryView(fromTargetRow(existing)), nil
		}

		return s.refreshGappedEvidence(ctx, projectID, existing)
	case err != nil && !errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeUnexpected, err, "error reading approval request").LogError(ctx, s.logger)
	}

	return s.admit(ctx, projectID, organizationID, admission{
		targetKind:      targetKindServerURL,
		targetRaw:       display,
		targetKey:       key,
		status:          statusUnreviewed,
		bypassRequestID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		requesterID:     "",
		requesterEmail:  nil,
		note:            nil,
		actor:           authCtx.UserID,
		actorEmail:      authCtx.Email,
	})
}

// storedEvidenceHasGaps reports whether a stored evidence document recorded
// sources it could not consult. A document that will not decode — or whose
// version this build no longer reads — reports gapped, so the next view
// retries the gather rather than pinning an unreadable document forever.
func storedEvidenceHasGaps(raw []byte, version int32) bool {
	document, err := evidence.DecodeDocument(raw, int(version))
	if err != nil {
		return true
	}

	return len(document.Gaps) > 0
}

// refreshGappedEvidence re-runs the gather for a dossier whose stored
// document recorded source gaps. The never-worse property holds by
// construction: the fresh document replaces the stored one only when it
// closed every gap, so a retry during the same outage — or a new one — leaves
// the richer stored document standing. This is still the read path: no audit
// entry is written, and the request's status and requesters are untouched.
func (s *Service) refreshGappedEvidence(ctx context.Context, projectID uuid.UUID, existing repo.GetApprovalRequestByTargetRow) (*gen.ApprovalRequestSummary, error) {
	// A view landing inside the cooldown reads the gapped document as-is,
	// so a persistent outage costs one gather per cooldown per replica
	// rather than one per page view.
	if !s.beginGapRetry(existing.ID) {
		return summaryView(fromTargetRow(existing)), nil
	}

	gatherCtx, cancelGather := context.WithTimeout(ctx, gatherTimeout)
	defer cancelGather()

	document, err := s.evidence.Assemble(gatherCtx, projectID, identity.Resolve(existing.TargetRaw))
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to re-assemble approval evidence", attr.SlogError(err))

		return summaryView(fromTargetRow(existing)), nil
	}

	if refreshed, decodeErr := evidence.DecodeDocument(document, evidence.Version); decodeErr != nil || len(refreshed.Gaps) > 0 {
		return summaryView(fromTargetRow(existing)), nil
	}

	// The write is a compare-and-set against the document this refresh read:
	// when a concurrent refresh already replaced the evidence, the slower
	// gather matches zero rows instead of clobbering the newer document, and
	// the re-read below returns the winner's evidence either way.
	queries := repo.New(s.db)
	if _, err := queries.RefreshApprovalRequestEvidence(ctx, repo.RefreshApprovalRequestEvidenceParams{
		CurrentEvidence:     document,
		EvidenceVersion:     evidence.Version,
		ID:                  existing.ID,
		ProjectID:           projectID,
		ObservedCollectedAt: existing.EvidenceCollectedAt,
	}); err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error storing evidence").LogError(ctx, s.logger)
	}

	row, err := queries.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: existing.ID, ProjectID: projectID})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error reading approval request").LogError(ctx, s.logger)
	}

	return summaryView(fromGetRow(row)), nil
}

// beginGapRetry reports whether a gap retry for this dossier may run now,
// recording the attempt when it may. Entries past their cooldown are pruned
// on the way through, so the map holds at most the gapped dossiers viewed
// within the current window.
func (s *Service) beginGapRetry(id uuid.UUID) bool {
	now := time.Now()

	s.gapRetryMu.Lock()
	defer s.gapRetryMu.Unlock()

	if last, seen := s.gapRetryAt[id]; seen && now.Sub(last) < gapRetryCooldown {
		return false
	}

	for other, last := range s.gapRetryAt {
		if now.Sub(last) >= gapRetryCooldown {
			delete(s.gapRetryAt, other)
		}
	}

	s.gapRetryAt[id] = now

	return true
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
	if len(raw) > maxTargetLength {
		return nil, oops.E(oops.CodeBadRequest, nil, "target must be at most %d characters", maxTargetLength)
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
		// The stored reference is the redacted form for the same reason: a
		// launch command routinely embeds credentials (`--header
		// "Authorization: Bearer …"`, `--api-key=…`, `TOKEN=… npx …`), and
		// target_raw reaches the queue, the audit feed, and the webhook
		// stream. RedactCommand also collapses whitespace, so it doubles as
		// the dedupe key: the same command with rotated tokens — or cosmetic
		// spacing differences — stays one review.
		raw = identity.RedactCommand(raw)
		key = raw
	default:
		return nil, oops.E(oops.CodeBadRequest, nil, "target_kind must be server_url or stdio_command")
	}

	// The justification is the one input no automated evidence supplies, so
	// a proactive ask cannot omit it.
	trimmedNote := strings.TrimSpace(payload.Note)
	if trimmedNote == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "a justification is required")
	}
	if len(trimmedNote) > maxNoteLength {
		return nil, oops.E(oops.CodeBadRequest, nil, "note must be at most %d characters", maxNoteLength)
	}
	note := &trimmedNote

	return s.admit(ctx, projectID, authCtx.ActiveOrganizationID, admission{
		targetKind:      payload.TargetKind,
		targetRaw:       raw,
		targetKey:       key,
		status:          statusRequested,
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
		status:          statusRequested,
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

	return &gen.ListApprovalRequestsResult{Requests: requests}, nil
}

func (s *Service) GetRequest(ctx context.Context, payload *gen.GetRequestPayload) (*gen.ApprovalRequestDetail, error) {
	projectID, _, err := s.project(ctx, authz.ScopeMCPApprovalRead)
	if err != nil {
		return nil, err
	}

	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid approval request id").LogError(ctx, s.logger)
	}

	return s.requestDetail(ctx, projectID, requestID)
}

// RefreshEvidence re-runs every evidence source for a request and replaces its
// current evidence with the fresh gather.
//
// It is gated on the read scope, not decide: gathering is not privileged —
// intake runs the identical gather for any authenticated member — and a
// reviewer preparing the queue must be able to refresh what they are reading.
// Nothing org-authored is written and frozen decision snapshots are never
// touched, which is also why no audit event is emitted. Unlike intake, where a
// failed gather must not lose the admission, an explicit refresh that gathered
// nothing reports the failure instead of silently keeping stale evidence —
// both when the assembler itself errors and when the gather ran but gapped on
// every remote source while the stored document did not: an all-gaps document
// carries strictly less than what it would replace, so the write is skipped
// and the failure surfaced.
func (s *Service) RefreshEvidence(ctx context.Context, payload *gen.RefreshEvidencePayload) (*gen.ApprovalRequestDetail, error) {
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
	// id from a dashboard URL cannot refresh another tenant's request.
	row, err := queries.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: projectID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, err, "approval request not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "error reading approval request").LogError(ctx, s.logger)
	}

	// The stored reference is what intake resolved and gathered from — the
	// redacted URL for a server_url target — so a refresh sees exactly what a
	// fresh admission of the same reference would.
	resolved := identity.Resolve(row.TargetRaw)

	gatherCtx, cancelGather := context.WithTimeout(ctx, gatherTimeout)
	defer cancelGather()
	document, err := s.evidence.Assemble(gatherCtx, projectID, resolved)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error gathering evidence").LogError(ctx, s.logger)
	}

	// A gather that gapped on every remote source learned nothing a reader
	// does not already know from the gaps alone. Writing it over a stored
	// document that did consult those sources would clobber real evidence
	// with a page of failures, so the refresh reports the outage instead —
	// unless no gather has ever landed (EvidenceCollectedAt unset) or the
	// stored document is equally gapped or from an older shape, where the
	// fresh gather is at least as informative.
	if fresh, decodeErr := evidence.DecodeDocument(document, evidence.Version); decodeErr == nil && fresh.GappedOnAllRemoteSources() && row.EvidenceCollectedAt.Valid {
		stored, storedErr := evidence.DecodeDocument(row.CurrentEvidence, int(row.EvidenceVersion))
		if storedErr == nil && !stored.GappedOnAllRemoteSources() {
			return nil, oops.E(oops.CodeUnexpected, nil, "every remote evidence source was unreachable; keeping the existing evidence").LogError(ctx, s.logger)
		}
	}

	// Compare-and-set against the gather that was current when this refresh
	// started: two concurrent refreshes race the network for seconds, and an
	// unconditional write would let whichever finished last — not whichever
	// gathered last — win. Losing the race is fine; the winner's evidence is
	// at least as fresh, and the detail below returns it.
	written, err := queries.SetApprovalRequestEvidenceIfUnchanged(ctx, repo.SetApprovalRequestEvidenceIfUnchangedParams{
		CurrentEvidence:     document,
		EvidenceVersion:     evidence.Version,
		ID:                  requestID,
		ProjectID:           projectID,
		PreviousCollectedAt: row.EvidenceCollectedAt,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "error storing evidence").LogError(ctx, s.logger)
	}
	if written == 0 {
		s.logger.InfoContext(ctx, "discarded refresh gather superseded by a concurrent write", attr.SlogMCPApprovalRequestID(requestID.String()))
	}

	return s.requestDetail(ctx, projectID, requestID)
}

// requestDetail assembles the full detail view of one request, already
// resolved to the caller's project.
func (s *Service) requestDetail(ctx context.Context, projectID uuid.UUID, requestID uuid.UUID) (*gen.ApprovalRequestDetail, error) {
	// The detail is assembled from four separate pool reads rather than one
	// snapshot, so a decision committing mid-read can appear in the decision
	// list before the request's status reflects it. Accepted for a dashboard
	// read: the skew is transient and a refresh converges.
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
		return nil, oops.E(oops.CodeBadRequest, nil, "decision must be approved or denied").LogError(ctx, s.logger)
	}

	// The rationale is the artifact cited when explaining the decision to the
	// requester, so a blank one is rejected rather than recorded.
	rationale := strings.TrimSpace(payload.Rationale)
	if rationale == "" {
		return nil, oops.E(oops.CodeBadRequest, nil, "a rationale is required").LogError(ctx, s.logger)
	}

	requestID, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid approval request id").LogError(ctx, s.logger)
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
			return nil, oops.E(oops.CodeBadRequest, err, "invalid research report id").LogError(ctx, s.logger)
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
				return nil, oops.E(oops.CodeBadRequest, nil, "research report does not belong to this request").LogError(ctx, s.logger)
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
