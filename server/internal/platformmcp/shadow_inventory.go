package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/officiallink"
)

const (
	shadowTargetKindServerURL    = "server_url"
	shadowTargetKindStdioCommand = "stdio_command"
	shadowTargetReferenceKind    = "shadow_mcp_target"
	shadowInventoryCursorKind    = "shadow_mcp_cursor"
	shadowInventoryPageSize      = 50
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
	ProjectSlug string `json:"project_slug" jsonschema:"the project's slug, as list_projects returns it"`
	// ProjectID replaces ProjectSlug before the handler runs: the ID of the project it names.
	ProjectID string `json:"-"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"opaque cursor returned by the preceding list_shadow_mcp_inventory page"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum targets to return; server clamps this to 50"`
}

type GetShadowMCPReviewInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"the project's slug, as list_projects returns it"`
	// ProjectID replaces ProjectSlug before the handler runs: the ID of the project it names.
	ProjectID       string `json:"-"`
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

	// OfficialServer is the project MCP server linked as this server's
	// official version. Remote targets only.
	OfficialServer *ShadowMCPOfficialServerLink `json:"official_server,omitempty" jsonschema:"the project MCP server linked as this server's official version; while link_state is on, direct use is blocked and agents are sent to it"`

	// SuggestedOfficialServer is a project MCP server that proxies to the
	// same upstream, offered while nothing is linked.
	SuggestedOfficialServer *ShadowMCPSuggestedOfficialServer `json:"suggested_official_server,omitempty" jsonschema:"a project MCP server that serves the same upstream, offered as the server to link while nothing is linked"`

	// OfficialLinkVersion is the token set_shadow_mcp_official_server takes
	// as expected_version. Remote targets only.
	OfficialLinkVersion string `json:"official_link_version,omitempty" jsonschema:"opaque version to pass as expected_version to set_shadow_mcp_official_server; remote targets only"`
}

// ShadowMCPOfficialServerLink is a Shadow MCP server's link to its official
// version.
type ShadowMCPOfficialServerLink struct {
	MCPID     string `json:"mcp_id" jsonschema:"the linked MCP server's ID, as find_mcp returns it"`
	Name      string `json:"name"`
	Slug      string `json:"slug,omitempty"`
	LinkState string `json:"link_state" jsonschema:"on: direct use is blocked and agents are sent to the official server; off: the link is kept but direct use follows the ordinary Shadow MCP rules"`
}

// ShadowMCPSuggestedOfficialServer is a project MCP server that can be
// linked as a Shadow MCP server's official version.
type ShadowMCPSuggestedOfficialServer struct {
	MCPID string `json:"mcp_id" jsonschema:"the MCP server's ID, to pass as mcp_id to set_shadow_mcp_official_server"`
	Name  string `json:"name"`
	Slug  string `json:"slug,omitempty"`
}

type ListShadowMCPInventoryOutput struct {
	Project RiskProject `json:"project"`
	// DefaultAccess is what the project does with a server nobody decided
	// on. Absent when it could not be read.
	DefaultAccess *ShadowMCPDefaultAccess  `json:"default_access,omitempty" jsonschema:"what the project does with an MCP server nobody decided on; absent when it could not be read"`
	Targets       []ShadowMCPTargetSummary `json:"targets"`
	NextCursor    string                   `json:"next_cursor,omitempty"`
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
	Project               RiskProject              `json:"project"`
	DefaultAccess         *ShadowMCPDefaultAccess  `json:"default_access,omitempty" jsonschema:"what the project does with an MCP server nobody decided on; absent when it could not be read"`
	Target                ShadowMCPTargetSummary   `json:"target"`
	Evidence              ShadowMCPEvidenceSummary `json:"evidence"`
	DistributionAdmission *DistributionAdmission   `json:"distribution_admission,omitempty"`
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
	projects                  riskProjectResolver
	inventory                 shadowInventoryReader
	reviews                   shadowReviewReader
	budget                    OperationBudget
	references                *subjectReferenceCodec
	versions                  *shadowDecisionVersionCodec
	now                       func() time.Time
	distributionAdmissionRead distributionAdmissionReader
	settings                  *pgxpool.Pool
}

func NewShadowInventoryService(dbReader shadowInventoryReader, reviews shadowReviewReader, dbQueries *platformrepo.Queries, budget OperationBudget, keyMaterial string) (*ShadowInventoryService, error) {
	codec, err := newSubjectReferenceCodec(keyMaterial)
	if err != nil {
		return nil, ErrShadowInventoryUnavailable
	}
	versions, err := newShadowDecisionVersionCodec(keyMaterial)
	if err != nil {
		return nil, ErrShadowInventoryUnavailable
	}
	if dbReader == nil || reviews == nil || dbQueries == nil || !budget.valid() {
		return nil, ErrShadowInventoryUnavailable
	}
	return &ShadowInventoryService{
		projects: postgresRiskProjectResolver{queries: dbQueries}, inventory: dbReader, reviews: reviews,
		budget: budget, references: codec, versions: versions, now: time.Now, distributionAdmissionRead: nil, settings: nil,
	}, nil
}

func (s *ShadowInventoryService) WithDistributionAdmissionReads(read distributionAdmissionReader) *ShadowInventoryService {
	if s != nil {
		s.distributionAdmissionRead = read
	}
	return s
}

// WithSettingsReads lets the inventory and the review report the project's
// default access and each server's official link version.
func (s *ShadowInventoryService) WithSettingsReads(db *pgxpool.Pool) *ShadowInventoryService {
	if s != nil {
		s.settings = db
	}
	return s
}

// defaultAccess reads the project's default access, or nil when it cannot
// be read: the inventory still answers without it.
func (s *ShadowInventoryService) defaultAccess(ctx context.Context, principal Principal, project ResolvedProject) *ShadowMCPDefaultAccess {
	if s.settings == nil {
		return nil
	}
	access, err := readShadowDefaultAccess(ctx, s.settings, principal.OrganizationID, project.ID, s.versions)
	if err != nil {
		return nil
	}
	return &access
}

func (s *ShadowInventoryService) valid() bool {
	return s != nil && s.projects != nil && s.inventory != nil && s.reviews != nil && s.budget.valid() && s.references != nil && s.versions != nil && s.now != nil
}

func (s *ShadowInventoryService) List(ctx context.Context, principal Principal, input ListShadowMCPInventoryInput) (ListShadowMCPInventoryOutput, error) {
	if !s.valid() || principal.OrganizationID == "" {
		return ListShadowMCPInventoryOutput{}, ErrShadowInventoryUnavailable
	}
	project, err := s.projects.Resolve(ctx, principal.OrganizationID, strings.TrimSpace(input.ProjectID), "")
	if err != nil {
		return ListShadowMCPInventoryOutput{}, mapShadowProjectError(err)
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
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
	output := ListShadowMCPInventoryOutput{Project: riskProject(project), DefaultAccess: s.defaultAccess(ctx, principal, project), Targets: make([]ShadowMCPTargetSummary, 0, len(selected)), NextCursor: ""}
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
	if err := s.budget.Allow(ctx, principal); err != nil {
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
	output := GetShadowMCPReviewOutput{Project: riskProject(project), DefaultAccess: s.defaultAccess(ctx, principal, project), Target: target, Evidence: emptyShadowEvidence(), DistributionAdmission: nil}
	if s.distributionAdmissionRead != nil {
		var distributionAdmission DistributionAdmission
		if targetKind == shadowTargetKindServerURL {
			distributionAdmission = s.distributionAdmissionRead.ForTarget(ctx, principal.OrganizationID, project.ID, targetKey)
		} else {
			distributionAdmission = s.distributionAdmissionRead.NotApplicable(ctx, principal.OrganizationID, project.ID)
		}
		output.DistributionAdmission = &distributionAdmission
	}
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
	result := ShadowMCPTargetSummary{
		Display: display, TargetKind: kind, ObservationState: observationState,
		FirstSeen: "", LastSeen: "", LastCalled: "", ObservedUseCount: max(row.ObservedUseCount, 0),
		UserCount: NewSubjectCount(int64(row.UserCount)),
		Access:    ShadowMCPAccessSummary{State: row.AccessSummary.State, AllowedFor: row.AccessSummary.AllowedFor, BlockedFor: row.AccessSummary.BlockedFor, BlockingDefault: row.AccessSummary.BlockingDefault, Decision: "", DecisionCoverage: row.AccessSummary.DecisionCoverage},
		Review:    nil, DecisionVersion: "", TargetReference: reference, ReferenceExpiresAt: now.Add(SubjectReferenceTTL).Format(time.RFC3339),
		OfficialServer: nil, SuggestedOfficialServer: nil, OfficialLinkVersion: "",
	}
	if kind == shadowTargetKindServerURL {
		if err := s.projectOfficialLink(project, row, &result); err != nil {
			return ShadowMCPTargetSummary{}, err
		}
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

// projectOfficialLink adds a remote target's official server link, the
// suggested server while nothing is linked, and the link's version.
func (s *ShadowInventoryService) projectOfficialLink(project ResolvedProject, row *accessgen.ShadowMCPInventoryServer, result *ShadowMCPTargetSummary) error {
	link := officiallink.Link{MCPServerID: uuid.Nil, AllowDirectAccess: false}
	if official := row.OfficialServer; official != nil {
		id, err := uuid.Parse(official.McpServerID)
		if err != nil {
			return ErrShadowInventoryUnavailable
		}
		link = officiallink.Link{MCPServerID: id, AllowDirectAccess: official.DirectAccessAllowed}
		result.OfficialServer = &ShadowMCPOfficialServerLink{MCPID: id.String(), Name: official.Name, Slug: conv.PtrValOr(official.Slug, ""), LinkState: shadowOfficialLinkState(link)}
	}
	if suggested := row.SuggestedOfficialServer; suggested != nil && result.OfficialServer == nil {
		result.SuggestedOfficialServer = &ShadowMCPSuggestedOfficialServer{MCPID: suggested.McpServerID, Name: suggested.Name, Slug: conv.PtrValOr(suggested.Slug, "")}
	}
	key, err := officiallink.Key(row.CanonicalServerURL)
	if err != nil {
		// A URL the link store cannot key cannot be linked either, so it
		// carries no version to change it with.
		return nil
	}
	version, err := s.versions.encodeSetting(shadowOfficialLinkVersionDomain, shadowOfficialLinkVersionState(project.ID, key, link))
	if err != nil {
		return ErrShadowInventoryUnavailable
	}
	result.OfficialLinkVersion = version
	return nil
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
	return kind == shadowTargetKindServerURL || kind == shadowTargetKindStdioCommand
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
