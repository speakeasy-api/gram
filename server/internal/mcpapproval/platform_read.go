package mcpapproval

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/evidence"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/researchagent"
	"github.com/speakeasy-api/gram/server/internal/oops"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

const unreadableEvidenceGap = "unreadable_evidence"

// PlatformReviewReadInput identifies one review for a trusted Platform runtime.
type PlatformReviewReadInput struct {
	OrganizationID string
	ProjectID      uuid.UUID
	TargetKind     string
	TargetKey      string
}

// PlatformReviewSummary is a deliberately lossy projection containing only
// closed classifications and aggregate counts.
type PlatformReviewSummary struct {
	Status                string   `json:"status"`
	StandingDecision      string   `json:"standing_decision,omitempty"`
	RequesterCount        int64    `json:"requester_count"`
	CreatedAt             string   `json:"created_at"`
	UpdatedAt             string   `json:"updated_at"`
	EvidenceCollected     bool     `json:"evidence_collected"`
	EvidenceChanged       bool     `json:"evidence_changed"`
	EvidenceGaps          []string `json:"evidence_gaps"`
	IdentityKind          string   `json:"identity_kind"`
	VersionPinned         bool     `json:"version_pinned"`
	PackagePublication    string   `json:"package_publication"`
	RepositoryState       string   `json:"repository_state"`
	AdvisoryLookup        string   `json:"advisory_lookup"`
	KnownAdvisories       int      `json:"known_advisories"`
	AuthorityState        string   `json:"authority_state"`
	CapabilitySource      string   `json:"capability_source"`
	DeclaredToolCount     int      `json:"declared_tool_count"`
	RiskyDeclarationCount int      `json:"risky_declaration_count"`
	ResearchStatus        string   `json:"research_status"`
	ResearchCoverage      string   `json:"research_coverage"`
	CitationCount         int      `json:"citation_count"`
	TrustedCitationCount  int      `json:"trusted_citation_count"`
}

// ReadPlatformReview returns a privacy-safe review summary for a Platform
// runtime that has already enforced live organization-admin authorization.
func (s *Service) ReadPlatformReview(ctx context.Context, input PlatformReviewReadInput) (PlatformReviewSummary, error) {
	if strings.TrimSpace(input.OrganizationID) == "" || input.ProjectID == uuid.Nil || strings.TrimSpace(input.TargetKey) == "" {
		return PlatformReviewSummary{}, oops.E(oops.CodeBadRequest, nil, "organization, project, and target key are required")
	}
	if input.TargetKind != targetKindServerURL && input.TargetKind != targetKindStdioCommand {
		return PlatformReviewSummary{}, oops.E(oops.CodeBadRequest, nil, "target_kind must be server_url or stdio_command")
	}

	if _, err := projectsrepo.New(s.db).GetProjectByIDAndOrganizationID(ctx, projectsrepo.GetProjectByIDAndOrganizationIDParams{ID: input.ProjectID, OrganizationID: input.OrganizationID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PlatformReviewSummary{}, oops.E(oops.CodeNotFound, err, "approval request not found")
		}
		return PlatformReviewSummary{}, oops.E(oops.CodeUnexpected, err, "error reading approval project").LogError(ctx, s.logger)
	}

	queries := repo.New(s.db)
	request, err := queries.GetApprovalRequestByTarget(ctx, repo.GetApprovalRequestByTargetParams{ProjectID: input.ProjectID, TargetKind: input.TargetKind, TargetKey: input.TargetKey})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PlatformReviewSummary{}, oops.E(oops.CodeNotFound, err, "approval request not found")
		}
		return PlatformReviewSummary{}, oops.E(oops.CodeUnexpected, err, "error reading approval request").LogError(ctx, s.logger)
	}
	if request.OrganizationID != input.OrganizationID {
		return PlatformReviewSummary{}, oops.E(oops.CodeNotFound, nil, "approval request not found")
	}

	decisions, err := queries.ListDecisionsForApprovalRequest(ctx, repo.ListDecisionsForApprovalRequestParams{McpApprovalRequestID: request.ID, ProjectID: input.ProjectID})
	if err != nil {
		return PlatformReviewSummary{}, oops.E(oops.CodeUnexpected, err, "error reading approval decisions").LogError(ctx, s.logger)
	}
	reports, err := queries.ListResearchReportsForApprovalRequest(ctx, repo.ListResearchReportsForApprovalRequestParams{McpApprovalRequestID: request.ID, ProjectID: input.ProjectID})
	if err != nil {
		return PlatformReviewSummary{}, oops.E(oops.CodeUnexpected, err, "error reading research reports").LogError(ctx, s.logger)
	}

	summary := newPlatformReviewSummary(request.Status)
	summary.RequesterCount = request.RequesterCount
	summary.CreatedAt = conv.FromPGTimestamptz(request.CreatedAt)
	summary.UpdatedAt = conv.FromPGTimestamptz(request.UpdatedAt)
	summary.EvidenceCollected = request.EvidenceCollectedAt.Valid
	summary.EvidenceChanged = request.EvidenceChangedAt.Valid
	if request.Status != statusSuperseded && len(decisions) > 0 {
		switch decisions[0].Decision {
		case decisionApproved, decisionDenied:
			summary.StandingDecision = decisions[0].Decision
		}
	}

	if summary.EvidenceCollected {
		document, decodeErr := evidence.DecodeDocument(request.CurrentEvidence, int(request.EvidenceVersion))
		if decodeErr != nil {
			summary.EvidenceGaps = []string{unreadableEvidenceGap}
		} else {
			projectPlatformEvidence(&summary, document)
		}
	}
	if len(reports) > 0 {
		projectPlatformResearch(&summary, reports[0])
	}

	return summary, nil
}

func newPlatformReviewSummary(status string) PlatformReviewSummary {
	switch status {
	case statusUnreviewed, statusRequested, statusSuperseded, decisionApproved, decisionDenied:
	default:
		status = "unknown"
	}
	return PlatformReviewSummary{
		Status: status, StandingDecision: "", RequesterCount: 0, CreatedAt: "", UpdatedAt: "",
		EvidenceCollected: false, EvidenceChanged: false, EvidenceGaps: []string{}, IdentityKind: "unknown", VersionPinned: false,
		PackagePublication: "unknown", RepositoryState: "unknown", AdvisoryLookup: "unknown", KnownAdvisories: 0,
		AuthorityState: "unknown", CapabilitySource: "unknown", DeclaredToolCount: 0, RiskyDeclarationCount: 0,
		ResearchStatus: "none", ResearchCoverage: "unknown", CitationCount: 0, TrustedCitationCount: 0,
	}
}

func projectPlatformEvidence(summary *PlatformReviewSummary, document evidence.Document) {
	summary.EvidenceGaps = closedPlatformEvidenceGaps(document.Gaps)
	switch document.Identity.Kind {
	case "unresolved", "remote", "package":
		summary.IdentityKind = document.Identity.Kind
	}
	summary.VersionPinned = document.Identity.VersionPinned
	if document.Package != nil {
		summary.PackagePublication = "published"
	} else if document.PackageNotPublished {
		summary.PackagePublication = "not_published"
	}
	if document.Repository != nil {
		summary.RepositoryState = "found"
	} else if document.RepositoryNotFound {
		summary.RepositoryState = "not_found"
	}
	if document.Advisories != nil {
		summary.AdvisoryLookup = "complete"
		summary.KnownAdvisories = max(document.Advisories.KnownCount, 0)
	}
	if document.Authority != nil {
		switch document.Authority.Mode {
		case "none":
			summary.AuthorityState = "none"
		case "api_key", "oauth":
			summary.AuthorityState = "declared"
		}
	}
	switch document.CapabilitiesSource {
	case evidence.CapabilitiesFromServer, evidence.CapabilitiesFromRegistry:
		summary.CapabilitySource = document.CapabilitiesSource
	}
	summary.DeclaredToolCount = len(document.Capabilities)
	for _, declaration := range document.Capabilities {
		if declaration.ActsOnBehalf || len(declaration.Declared) > 0 || len(declaration.SchemaImplied) > 0 {
			summary.RiskyDeclarationCount++
		}
	}
}

func closedPlatformEvidenceGaps(gaps []string) []string {
	closed := make([]string, 0, len(gaps))
	seen := make(map[string]struct{}, len(gaps))
	for _, gap := range gaps {
		switch gap {
		case evidence.GapPackageLookup, evidence.GapExposureLookup, evidence.GapAuthorityProbe,
			evidence.GapToolDeclarations, evidence.GapCatalogLookup, evidence.GapRepositoryLookup,
			evidence.GapAdvisoryLookup, evidence.GapDomainLookup:
		default:
			gap = unreadableEvidenceGap
		}
		if _, ok := seen[gap]; ok {
			continue
		}
		seen[gap] = struct{}{}
		closed = append(closed, gap)
	}
	return closed
}

func projectPlatformResearch(summary *PlatformReviewSummary, report repo.McpResearchReport) {
	status := report.Status
	if status == researchStatusRunning && report.StartedAt.Valid && time.Since(report.StartedAt.Time) > ResearchRunStaleAfter {
		status = "failed"
	}
	switch status {
	case researchStatusRunning, "failed", "completed":
		summary.ResearchStatus = status
	default:
		summary.ResearchStatus = "failed"
	}
	if status != "completed" || report.ReportVersion != researchagent.ReportVersion {
		return
	}

	var document researchagent.Document
	if err := json.Unmarshal(report.Report, &document); err != nil {
		return
	}
	switch document.Coverage.Level {
	case researchagent.CoverageNone, researchagent.CoverageThin, researchagent.CoverageModerate, researchagent.CoverageSubstantial:
		summary.ResearchCoverage = document.Coverage.Level
	}
	for _, claim := range document.Claims {
		for _, citation := range claim.Citations {
			summary.CitationCount++
			if strings.TrimSpace(citation.TrustedSource) != "" {
				summary.TrustedCitationCount++
			}
		}
	}
}
