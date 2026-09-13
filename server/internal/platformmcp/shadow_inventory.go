package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const (
	shadowTargetKindServerURL    = "server_url"
	shadowTargetKindStdioCommand = "stdio_command"
	// shadowTargetKindToolNamespace is an MCP server an LLM proxy saw only by
	// the <server> segment of its namespaced tool names, inventoried under
	// the synthetic identity mcp-tool://<server>. It has usage but no
	// resolved server identity.
	shadowTargetKindToolNamespace = "tool_namespace"
	shadowTargetReferenceKind     = "shadow_mcp_target"
	shadowInventoryCursorKind     = "shadow_mcp_cursor"
	shadowInventoryPageSize       = 50
)

var (
	ErrShadowInventoryInvalid     = errors.New("invalid platform mcp shadow inventory request")
	ErrShadowInventoryNotFound    = errors.New("platform mcp shadow inventory target not found")
	ErrShadowInventoryUnavailable = errors.New("platform mcp shadow inventory unavailable")
)

type shadowInventoryReader interface {
	ReadShadowMCPInventory(context.Context, access.ShadowMCPInventoryReadInput) (*accessgen.ListShadowMCPInventoryResult, error)
	ReadShadowMCPInventoryTarget(context.Context, access.ShadowMCPInventoryTargetInput) (*accessgen.ShadowMCPInventoryServer, error)
}

type shadowReviewReader interface {
	ReadPlatformReview(context.Context, mcpapproval.PlatformReviewReadInput) (mcpapproval.PlatformReviewSummary, error)
}

type ListShadowMCPInventoryInput struct {
	ProjectID string `json:"project_id" jsonschema:"explicit project ID returned by list_projects"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"opaque cursor returned by the preceding list_shadow_mcp_inventory page"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum targets to return; server clamps this to 50"`
}

type GetShadowMCPReviewInput struct {
	ProjectID       string `json:"project_id" jsonschema:"explicit project ID used to list this target"`
	TargetReference string `json:"target_reference" jsonschema:"short-lived opaque target reference returned by list_shadow_mcp_inventory"`
}

type ShadowMCPAccessSummary struct {
	State            string `json:"state"`
	AllowedFor       string `json:"allowed_for"`
	BlockedFor       string `json:"blocked_for"`
	BlockingDefault  string `json:"blocking_default"`
	Decision         string `json:"decision,omitempty"`
	DecisionCoverage string `json:"decision_coverage"`
}

type ShadowMCPReviewSummary struct {
	Status           string       `json:"status"`
	StandingDecision string       `json:"standing_decision,omitempty"`
	RequesterCount   SubjectCount `json:"requester_count"`
	EvidenceChanged  bool         `json:"evidence_changed"`
}

type ShadowMCPTargetSummary struct {
	Display            string                  `json:"display"`
	TargetKind         string                  `json:"target_kind"`
	ObservationState   string                  `json:"observation_state"`
	FirstSeen          string                  `json:"first_seen,omitempty"`
	LastSeen           string                  `json:"last_seen,omitempty"`
	LastCalled         string                  `json:"last_called,omitempty"`
	ObservedUseCount   int                     `json:"observed_use_count"`
	UserCount          SubjectCount            `json:"user_count"`
	Access             ShadowMCPAccessSummary  `json:"access"`
	Review             *ShadowMCPReviewSummary `json:"review,omitempty"`
	DecisionVersion    string                  `json:"decision_version,omitempty"`
	TargetReference    string                  `json:"target_reference"`
	ReferenceExpiresAt string                  `json:"reference_expires_at"`
}

type ListShadowMCPInventoryOutput struct {
	Project    RiskProject              `json:"project"`
	Targets    []ShadowMCPTargetSummary `json:"targets"`
	NextCursor string                   `json:"next_cursor,omitempty"`
}

type ShadowMCPEvidenceSummary struct {
	Collected             bool     `json:"collected"`
	Gaps                  []string `json:"gaps"`
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

type GetShadowMCPReviewOutput struct {
	Project  RiskProject              `json:"project"`
	Target   ShadowMCPTargetSummary   `json:"target"`
	Evidence ShadowMCPEvidenceSummary `json:"evidence"`
}

type shadowTargetReference struct {
	Kind string `json:"kind"`
	Key  string `json:"key"`
}

type shadowInventoryCursor struct {
	Offset       int    `json:"offset,omitempty"`
	AccessCursor string `json:"access_cursor,omitempty"`
}

type ShadowInventoryService struct {
	projects      riskProjectResolver
	inventory     shadowInventoryReader
	reviews       shadowReviewReader
	flags         feature.Provider
	organizations OrganizationSlugResolver
	budget        OperationBudget
	references    *subjectReferenceCodec
	versions      *shadowDecisionVersionCodec
	now           func() time.Time
}

func NewShadowInventoryService(dbReader shadowInventoryReader, reviews shadowReviewReader, flags feature.Provider, organizations OrganizationSlugResolver, dbQueries *platformrepo.Queries, budget OperationBudget, keyMaterial string) (*ShadowInventoryService, error) {
	codec, err := newSubjectReferenceCodec(keyMaterial)
	if err != nil {
		return nil, ErrShadowInventoryUnavailable
	}
	versions, err := newShadowDecisionVersionCodec(keyMaterial)
	if err != nil {
		return nil, ErrShadowInventoryUnavailable
	}
	if dbReader == nil || reviews == nil || flags == nil || organizations == nil || dbQueries == nil || !budget.valid() {
		return nil, ErrShadowInventoryUnavailable
	}
	return &ShadowInventoryService{
		projects: postgresRiskProjectResolver{queries: dbQueries}, inventory: dbReader, reviews: reviews,
		flags: flags, organizations: organizations, budget: budget, references: codec, versions: versions, now: time.Now,
	}, nil
}

func (s *ShadowInventoryService) valid() bool {
	return s != nil && s.projects != nil && s.inventory != nil && s.reviews != nil && s.flags != nil && s.organizations != nil && s.budget.valid() && s.references != nil && s.versions != nil && s.now != nil
}

func (s *ShadowInventoryService) List(ctx context.Context, principal Principal, input ListShadowMCPInventoryInput) (ListShadowMCPInventoryOutput, error) {
	if !s.valid() || principal.OrganizationID == "" {
		return ListShadowMCPInventoryOutput{}, ErrShadowInventoryUnavailable
	}
	project, err := s.projects.Resolve(ctx, principal.OrganizationID, strings.TrimSpace(input.ProjectID), "")
	if err != nil {
		return ListShadowMCPInventoryOutput{}, mapShadowProjectError(err)
	}
	if err := s.admit(ctx, principal, project); err != nil {
		return ListShadowMCPInventoryOutput{}, err
	}
	limit := input.Limit
	if limit <= 0 || limit > shadowInventoryPageSize {
		limit = shadowInventoryPageSize
	}
	scope := queryScope("shadow_inventory", project.ID.String())
	page := shadowInventoryCursor{Offset: 0, AccessCursor: ""}
	if input.Cursor != "" {
		encoded, err := s.references.DecodeScoped(input.Cursor, principal, shadowInventoryCursorKind, scope, s.now())
		if err != nil || json.Unmarshal([]byte(encoded), &page) != nil || page.Offset < 0 || (page.Offset > 0 && page.AccessCursor != "") {
			return ListShadowMCPInventoryOutput{}, ErrShadowInventoryInvalid
		}
	}
	accessCursor := page.AccessCursor
	var cursor *string
	if accessCursor != "" {
		cursor = &accessCursor
	}
	rows, err := s.inventory.ReadShadowMCPInventory(ctx, access.ShadowMCPInventoryReadInput{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, Limit: shadowInventoryPageSize, Cursor: cursor,
	})
	if err != nil {
		return ListShadowMCPInventoryOutput{}, mapShadowReadError(err)
	}
	if page.Offset > len(rows.Servers) {
		return ListShadowMCPInventoryOutput{}, ErrShadowInventoryInvalid
	}
	remaining := rows.Servers[page.Offset:]
	selected := remaining
	if len(selected) > limit {
		selected = selected[:limit]
	}
	output := ListShadowMCPInventoryOutput{Project: riskProject(project), Targets: make([]ShadowMCPTargetSummary, 0, len(selected)), NextCursor: ""}
	for _, row := range selected {
		target, err := s.projectTarget(principal, project, row)
		if err != nil {
			return ListShadowMCPInventoryOutput{}, err
		}
		output.Targets = append(output.Targets, target)
	}
	consumed := page.Offset + len(selected)
	next := shadowInventoryCursor{Offset: 0, AccessCursor: ""}
	switch {
	case consumed < len(rows.Servers):
		next.Offset = consumed
	case rows.NextCursor != nil && *rows.NextCursor != "":
		next.AccessCursor = *rows.NextCursor
	}
	if next.Offset > 0 || next.AccessCursor != "" {
		payload, err := json.Marshal(next)
		if err != nil {
			return ListShadowMCPInventoryOutput{}, fmt.Errorf("encode shadow inventory cursor: %w", err)
		}
		output.NextCursor, err = s.references.EncodeScoped(principal, shadowInventoryCursorKind, scope, string(payload), s.now())
		if err != nil {
			return ListShadowMCPInventoryOutput{}, ErrShadowInventoryUnavailable
		}
	}
	return output, nil
}

// ResolveTargetReference resolves a D1 handle for the D2 mutation path. The
// underlying URL or command remains inside the server process.
func (s *ShadowInventoryService) ResolveTargetReference(principal Principal, projectID, targetReference string) (string, string, error) {
	if !s.valid() {
		return "", "", ErrShadowInventoryUnavailable
	}
	projectID = strings.TrimSpace(projectID)
	scope := queryScope("shadow_target", projectID)
	encoded, err := s.references.DecodeScoped(strings.TrimSpace(targetReference), principal, shadowTargetReferenceKind, scope, s.now())
	if err != nil {
		return "", "", ErrShadowInventoryNotFound
	}
	var reference shadowTargetReference
	if json.Unmarshal([]byte(encoded), &reference) != nil || !validShadowTargetKind(reference.Kind) || reference.Key == "" {
		return "", "", ErrShadowInventoryNotFound
	}
	return reference.Kind, reference.Key, nil
}

func (s *ShadowInventoryService) GetReview(ctx context.Context, principal Principal, input GetShadowMCPReviewInput) (GetShadowMCPReviewOutput, error) {
	if !s.valid() || principal.OrganizationID == "" {
		return GetShadowMCPReviewOutput{}, ErrShadowInventoryUnavailable
	}
	project, err := s.projects.Resolve(ctx, principal.OrganizationID, strings.TrimSpace(input.ProjectID), "")
	if err != nil {
		return GetShadowMCPReviewOutput{}, mapShadowProjectError(err)
	}
	if err := s.admit(ctx, principal, project); err != nil {
		return GetShadowMCPReviewOutput{}, err
	}
	targetKind, targetKey, err := s.ResolveTargetReference(principal, project.ID.String(), input.TargetReference)
	if err != nil {
		return GetShadowMCPReviewOutput{}, err
	}
	row, err := s.inventory.ReadShadowMCPInventoryTarget(ctx, access.ShadowMCPInventoryTargetInput{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, TargetKind: targetKind, TargetKey: targetKey,
	})
	if err != nil {
		return GetShadowMCPReviewOutput{}, mapShadowReadError(err)
	}
	target, err := s.projectTarget(principal, project, row)
	if err != nil {
		return GetShadowMCPReviewOutput{}, err
	}
	output := GetShadowMCPReviewOutput{Project: riskProject(project), Target: target, Evidence: emptyShadowEvidence()}
	if row.ApprovalRequest == nil {
		return output, nil
	}
	review, err := s.reviews.ReadPlatformReview(ctx, mcpapproval.PlatformReviewReadInput{
		OrganizationID: principal.OrganizationID, ProjectID: project.ID, TargetKind: targetKind, TargetKey: targetKey,
	})
	if err != nil {
		return GetShadowMCPReviewOutput{}, mapShadowReadError(err)
	}
	output.Evidence = shadowEvidence(review)
	output.Target.DecisionVersion, err = s.versions.Encode(review.DecisionVersionState)
	if err != nil {
		return GetShadowMCPReviewOutput{}, ErrShadowInventoryUnavailable
	}
	return output, nil
}

func (s *ShadowInventoryService) admit(ctx context.Context, principal Principal, project ResolvedProject) error {
	organizationSlug, err := s.organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil || organizationSlug == "" {
		return ErrShadowInventoryUnavailable
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.flags, feature.FlagMCPApproval, principal.OrganizationID, feature.OrgProjectGroups(organizationSlug, ""))
	if err != nil {
		return ErrShadowInventoryUnavailable
	}
	if evaluation != feature.EvaluationEnabled {
		return ErrShadowInventoryUnavailable
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return err
	}
	return nil
}

func (s *ShadowInventoryService) projectTarget(principal Principal, project ResolvedProject, row *accessgen.ShadowMCPInventoryServer) (ShadowMCPTargetSummary, error) {
	if row == nil {
		return ShadowMCPTargetSummary{}, ErrShadowInventoryUnavailable
	}
	kind := shadowTargetKindServerURL
	if row.TargetKind != nil {
		kind = *row.TargetKind
	}
	if !validShadowTargetKind(kind) || row.CanonicalServerURL == "" || row.AccessSummary == nil {
		return ShadowMCPTargetSummary{}, ErrShadowInventoryUnavailable
	}
	referencePayload, err := json.Marshal(shadowTargetReference{Kind: kind, Key: row.CanonicalServerURL})
	if err != nil {
		return ShadowMCPTargetSummary{}, fmt.Errorf("encode shadow target reference: %w", err)
	}
	now := s.now().UTC()
	reference, err := s.references.EncodeScoped(principal, shadowTargetReferenceKind, queryScope("shadow_target", project.ID.String()), string(referencePayload), now)
	if err != nil {
		return ShadowMCPTargetSummary{}, ErrShadowInventoryUnavailable
	}
	observed := row.FirstSeen != "" && row.FirstSeen != time.Time{}.UTC().Format(time.RFC3339)
	observationState := "requested_only"
	display := "Requested MCP target"
	if observed {
		observationState = "observed"
		display = "Observed MCP server"
	}
	if kind == shadowTargetKindStdioCommand {
		display = "Requested local MCP command"
	}
	if kind == shadowTargetKindToolNamespace {
		display = "Observed MCP tool namespace (server identity unresolved)"
	}
	result := ShadowMCPTargetSummary{
		Display: display, TargetKind: kind, ObservationState: observationState,
		FirstSeen: "", LastSeen: "", LastCalled: "", ObservedUseCount: max(row.ObservedUseCount, 0),
		UserCount: NewSubjectCount(int64(row.UserCount)),
		Access:    ShadowMCPAccessSummary{State: row.AccessSummary.State, AllowedFor: row.AccessSummary.AllowedFor, BlockedFor: row.AccessSummary.BlockedFor, BlockingDefault: row.AccessSummary.BlockingDefault, Decision: "", DecisionCoverage: row.AccessSummary.DecisionCoverage},
		Review:    nil, DecisionVersion: "", TargetReference: reference, ReferenceExpiresAt: now.Add(SubjectReferenceTTL).Format(time.RFC3339),
	}
	if !validShadowAccessSummary(result.Access) {
		return ShadowMCPTargetSummary{}, ErrShadowInventoryUnavailable
	}
	if row.AccessSummary.Decision != nil {
		if !validShadowDecision(*row.AccessSummary.Decision) {
			return ShadowMCPTargetSummary{}, ErrShadowInventoryUnavailable
		}
		result.Access.Decision = *row.AccessSummary.Decision
	}
	if observed {
		result.FirstSeen, result.LastSeen = row.FirstSeen, row.LastSeen
		if row.LastCalled != nil {
			result.LastCalled = *row.LastCalled
		}
	}
	if row.ApprovalRequest != nil {
		if !validShadowReviewStatus(row.ApprovalRequest.Status) {
			return ShadowMCPTargetSummary{}, ErrShadowInventoryUnavailable
		}
		result.Review = &ShadowMCPReviewSummary{
			Status: row.ApprovalRequest.Status, StandingDecision: "", RequesterCount: NewSubjectCount(int64(row.ApprovalRequest.RequesterCount)), EvidenceChanged: row.ApprovalRequest.EvidenceChangedAt != nil,
		}
		if row.ApprovalRequest.StandingDecision != nil {
			if !validShadowDecision(*row.ApprovalRequest.StandingDecision) {
				return ShadowMCPTargetSummary{}, ErrShadowInventoryUnavailable
			}
			result.Review.StandingDecision = *row.ApprovalRequest.StandingDecision
		}
	}
	return result, nil
}

func emptyShadowEvidence() ShadowMCPEvidenceSummary {
	return ShadowMCPEvidenceSummary{
		Collected: false, Gaps: []string{}, IdentityKind: "unknown", VersionPinned: false,
		PackagePublication: "unknown", RepositoryState: "unknown", AdvisoryLookup: "unknown", KnownAdvisories: 0,
		AuthorityState: "unknown", CapabilitySource: "unknown", DeclaredToolCount: 0, RiskyDeclarationCount: 0,
		ResearchStatus: "none", ResearchCoverage: "unknown", CitationCount: 0, TrustedCitationCount: 0,
	}
}

func shadowEvidence(review mcpapproval.PlatformReviewSummary) ShadowMCPEvidenceSummary {
	if !validShadowEvidence(review) {
		return emptyShadowEvidence()
	}
	return ShadowMCPEvidenceSummary{
		Collected: review.EvidenceCollected, Gaps: append([]string{}, review.EvidenceGaps...), IdentityKind: review.IdentityKind,
		VersionPinned: review.VersionPinned, PackagePublication: review.PackagePublication, RepositoryState: review.RepositoryState,
		AdvisoryLookup: review.AdvisoryLookup, KnownAdvisories: review.KnownAdvisories, AuthorityState: review.AuthorityState,
		CapabilitySource: review.CapabilitySource, DeclaredToolCount: review.DeclaredToolCount, RiskyDeclarationCount: review.RiskyDeclarationCount,
		ResearchStatus: review.ResearchStatus, ResearchCoverage: review.ResearchCoverage, CitationCount: review.CitationCount,
		TrustedCitationCount: review.TrustedCitationCount,
	}
}

func validShadowEvidence(review mcpapproval.PlatformReviewSummary) bool {
	if !oneOf(review.IdentityKind, "unknown", "unresolved", "remote", "package") ||
		!oneOf(review.PackagePublication, "unknown", "published", "not_published") ||
		!oneOf(review.RepositoryState, "unknown", "found", "not_found") ||
		!oneOf(review.AdvisoryLookup, "unknown", "complete") ||
		!oneOf(review.AuthorityState, "unknown", "declared", "none") ||
		!oneOf(review.CapabilitySource, "unknown", "server", "registry") ||
		!oneOf(review.ResearchStatus, "none", "running", "failed", "completed") ||
		!oneOf(review.ResearchCoverage, "unknown", "none", "thin", "moderate", "substantial") {
		return false
	}
	for _, gap := range review.EvidenceGaps {
		if !oneOf(gap, "package_lookup_failed", "exposure_lookup_failed", "authority_probe_failed", "tool_declarations_probe_failed", "catalog_lookup_failed", "repository_lookup_failed", "advisory_lookup_failed", "domain_lookup_failed", "unreadable_evidence") {
			return false
		}
	}
	return true
}

func validShadowTargetKind(kind string) bool {
	return kind == shadowTargetKindServerURL || kind == shadowTargetKindStdioCommand || kind == shadowTargetKindToolNamespace
}

func validShadowAccessSummary(summary ShadowMCPAccessSummary) bool {
	return oneOf(summary.State, "allowed", "restricted", "blocked", "unenforced") &&
		oneOf(summary.AllowedFor, "everyone", "selected", "none") &&
		oneOf(summary.BlockedFor, "everyone", "some", "none") &&
		oneOf(summary.BlockingDefault, "deny", "allow", "none") &&
		oneOf(summary.DecisionCoverage, "full", "partial", "none")
}

func validShadowReviewStatus(status string) bool {
	return oneOf(status, "unreviewed", "requested", "approved", "denied", "superseded")
}

func validShadowDecision(decision string) bool {
	return oneOf(decision, "approved", "denied")
}

func oneOf(value string, allowed ...string) bool {
	return slices.Contains(allowed, value)
}

func mapShadowProjectError(err error) error {
	switch {
	case errors.Is(err, ErrRiskReadInvalid):
		return ErrShadowInventoryInvalid
	case errors.Is(err, ErrRiskReadNotFound):
		return ErrShadowInventoryNotFound
	default:
		return fmt.Errorf("resolve shadow mcp project: %w", ErrShadowInventoryUnavailable)
	}
}

func mapShadowReadError(err error) error {
	if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shareable.Code {
		case oops.CodeBadRequest, oops.CodeInvalid:
			return ErrShadowInventoryInvalid
		case oops.CodeNotFound:
			return ErrShadowInventoryNotFound
		default:
			return fmt.Errorf("read shadow mcp inventory: %w", ErrShadowInventoryUnavailable)
		}
	}
	return fmt.Errorf("read shadow mcp inventory: %w", ErrShadowInventoryUnavailable)
}
