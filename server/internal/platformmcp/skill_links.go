//nolint:exhaustruct,wrapcheck // Zero-value summaries are the documented "absent" signal, and a skills-service refusal is returned unwrapped so its typed code survives the trip to the tool result.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	genskills "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
)

// maxSkillLinkLookup bounds the links one skill is read back with. The
// skills service caps a page at 50, and a skill served by more surfaces than
// that is reported as truncated rather than paged through.
const maxSkillLinkLookup = 50

var (
	// ErrSkillMCPTargetNotFound is an exact MCP server or gateway that does
	// not exist in the named project. It is never softened into another
	// surface.
	ErrSkillMCPTargetNotFound = errors.New("platform mcp skill link target not found")
	// ErrSkillMCPTargetAmbiguous is a name matching more than one MCP server
	// or gateway.
	ErrSkillMCPTargetAmbiguous = errors.New("platform mcp skill link target ambiguous")
	// ErrSkillLinkNotConfirmed is a link write whose result the post-write
	// read could not find.
	ErrSkillLinkNotConfirmed = errors.New("platform mcp skill link not visible after write")
)

// SkillMCPTargetKind names the kind of MCP surface a skill is served by.
type SkillMCPTargetKind string

const (
	SkillMCPTargetServer              SkillMCPTargetKind = "mcp_server"
	SkillMCPTargetGateway             SkillMCPTargetKind = "meta_mcp_server"
	SkillMCPTargetOrganizationGateway SkillMCPTargetKind = "organization_gateway"
)

// SkillMCPTarget is one MCP surface a skill can be served by. The
// organization gateway has no row, so it carries neither an id nor a name.
type SkillMCPTarget struct {
	Kind SkillMCPTargetKind `json:"kind"`
	ID   string             `json:"id,omitempty"`
	Name string             `json:"name,omitempty"`
	Slug string             `json:"slug,omitempty"`
}

// SkillMCPTargetInventory names the MCP servers and gateways a skill may be
// linked to in one project. It is optional on SkillsService: without it the
// link tools refuse as unavailable.
type SkillMCPTargetInventory interface {
	SkillMCPTargets(ctx context.Context, organizationID string, projectID uuid.UUID, limit int) ([]SkillMCPTarget, error)
}

// SkillMCPLinkSummary is one live link: which skill an MCP surface serves, and
// whether it serves a pinned version or the latest valid one.
type SkillMCPLinkSummary struct {
	LinkID        string         `json:"link_id"`
	SkillID       string         `json:"skill_id"`
	SkillName     string         `json:"skill_name"`
	Target        SkillMCPTarget `json:"target"`
	CreatedAt     string         `json:"created_at"`
	FollowsLatest bool           `json:"follows_latest"`

	// PinnedVersionID is set only when the link serves one fixed version.
	PinnedVersionID string `json:"pinned_version_id,omitempty"`
}

func buildSkillMCPLinkSummary(link *types.SkillLink) SkillMCPLinkSummary {
	target := SkillMCPTarget{Kind: SkillMCPTargetKind(link.TargetKind), Name: stringOrEmpty(link.TargetName)}
	switch target.Kind {
	case SkillMCPTargetServer:
		target.ID = stringOrEmpty(link.McpServerID)
	case SkillMCPTargetGateway:
		target.ID = stringOrEmpty(link.MetaMcpServerID)
	case SkillMCPTargetOrganizationGateway:
	}
	return SkillMCPLinkSummary{
		LinkID:          link.ID,
		SkillID:         link.SkillID,
		SkillName:       link.SkillName,
		Target:          target,
		CreatedAt:       link.CreatedAt,
		FollowsLatest:   link.PinnedVersionID == nil,
		PinnedVersionID: stringOrEmpty(link.PinnedVersionID),
	}
}

// skillLinkFilter is the listLinks payload that selects one skill's link to
// one exact target.
func skillLinkFilter(skillID string, target SkillMCPTarget) *genskills.ListLinksPayload {
	kind := string(target.Kind)
	payload := &genskills.ListLinksPayload{
		SkillID:          &skillID,
		McpServerID:      nil,
		MetaMcpServerID:  nil,
		TargetKind:       &kind,
		Cursor:           nil,
		Limit:            maxSkillLinkLookup,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
	switch target.Kind {
	case SkillMCPTargetServer:
		payload.McpServerID = &target.ID
	case SkillMCPTargetGateway:
		payload.MetaMcpServerID = &target.ID
	case SkillMCPTargetOrganizationGateway:
	}
	return payload
}

// servedBy reads the live links of one skill, or of every skill in the
// project when skillID is empty. It is advisory on a distribution listing:
// when serving skills over MCP is off for the organization, nothing is served
// that way, so the answer is an empty list rather than a refusal.
func (s *SkillsService) servedBy(ctx context.Context, skillID string) ([]SkillMCPLinkSummary, bool, error) {
	result, err := s.skills.ListLinks(ctx, &genskills.ListLinksPayload{
		SkillID:          optionalString(skillID),
		McpServerID:      nil,
		MetaMcpServerID:  nil,
		TargetKind:       nil,
		Cursor:           nil,
		Limit:            maxSkillLinkLookup,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if errors.Is(err, skills.ErrSkillsOverMCPDisabled) {
		return []SkillMCPLinkSummary{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	links := make([]SkillMCPLinkSummary, 0, len(result.Links))
	for _, link := range result.Links {
		if link != nil {
			links = append(links, buildSkillMCPLinkSummary(link))
		}
	}
	return links, result.NextCursor != nil, nil
}

// resolveMCPTarget matches one MCP server or gateway exactly, by id, slug or
// exact name, or names the organization gateway. A name matching nothing is
// not_found and one matching more than one surface is ambiguous; neither
// falls back to another surface.
func (s *SkillsService) resolveMCPTarget(ctx context.Context, principal Principal, project ResolvedProject, kind, mcpServer, gateway string) (SkillMCPTarget, error) {
	mcpServer = strings.TrimSpace(mcpServer)
	gateway = strings.TrimSpace(gateway)
	var wanted string
	switch SkillMCPTargetKind(strings.TrimSpace(kind)) {
	case SkillMCPTargetServer:
		if mcpServer == "" || gateway != "" {
			return SkillMCPTarget{}, ErrRegistrationInvalid
		}
		wanted = mcpServer
	case SkillMCPTargetGateway:
		if gateway == "" || mcpServer != "" {
			return SkillMCPTarget{}, ErrRegistrationInvalid
		}
		wanted = gateway
	case SkillMCPTargetOrganizationGateway:
		if mcpServer != "" || gateway != "" {
			return SkillMCPTarget{}, ErrRegistrationInvalid
		}
		return SkillMCPTarget{Kind: SkillMCPTargetOrganizationGateway}, nil
	default:
		return SkillMCPTarget{}, ErrRegistrationInvalid
	}

	candidates, err := s.mcpTargets.SkillMCPTargets(ctx, principal.OrganizationID, project.ID, maxSkillTargetLookup)
	if err != nil {
		return SkillMCPTarget{}, err
	}
	matches := make([]SkillMCPTarget, 0, 2)
	for _, candidate := range candidates {
		if string(candidate.Kind) == strings.TrimSpace(kind) && matchesTargetName(candidate.ID, candidate.Slug, candidate.Name, wanted) {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 0:
		return SkillMCPTarget{}, ErrSkillMCPTargetNotFound
	case 1:
		return matches[0], nil
	default:
		return SkillMCPTarget{}, ErrSkillMCPTargetAmbiguous
	}
}

// LinkSkillToMCPInput names one skill and exactly one MCP surface to serve it.
type LinkSkillToMCPInput struct {
	ProjectSlug     string
	SkillID         string
	TargetKind      string
	MCPServer       string
	Gateway         string
	PinnedVersionID string
	IdempotencyKey  string
}

// LinkSkillToMCPOutput is the receipt for a link, read back after the write.
type LinkSkillToMCPOutput struct {
	ProjectSlug string              `json:"project_slug"`
	Link        SkillMCPLinkSummary `json:"link"`
	Message     string              `json:"message"`
}

// LinkSkillToMCP links a skill to one MCP server, gateway or the organization
// gateway, so that surface serves it over MCP. The skills service enforces
// the rollout, skill read and the write permission the target demands, and
// treats a repeat for the same skill and target as an update of the pin or a
// no-op, so the call is safe to retry.
func (s *SkillsService) LinkSkillToMCP(ctx context.Context, principal Principal, input LinkSkillToMCPInput) (LinkSkillToMCPOutput, error) {
	ctx, project, err := s.begin(ctx, principal, input.ProjectSlug)
	if err != nil {
		return LinkSkillToMCPOutput{}, err
	}
	if s.mcpTargets == nil {
		return LinkSkillToMCPOutput{}, ErrSkillsUnavailable
	}
	if _, err := uuid.Parse(input.SkillID); err != nil {
		return LinkSkillToMCPOutput{}, ErrRegistrationInvalid
	}
	pinned := strings.TrimSpace(input.PinnedVersionID)
	if pinned != "" {
		if _, err := uuid.Parse(pinned); err != nil {
			return LinkSkillToMCPOutput{}, ErrRegistrationInvalid
		}
	}
	if key := strings.TrimSpace(input.IdempotencyKey); key == "" || len(key) > maxSkillIdempotencyKeyLength {
		return LinkSkillToMCPOutput{}, ErrRegistrationInvalid
	}
	// Skill read is checked before the target is resolved, so a caller
	// without it is refused the same way whether or not the named server
	// exists.
	if err := s.requireSkillRead(ctx, project, input.SkillID); err != nil {
		return LinkSkillToMCPOutput{}, err
	}
	target, err := s.resolveMCPTarget(ctx, principal, project, input.TargetKind, input.MCPServer, input.Gateway)
	if err != nil {
		return LinkSkillToMCPOutput{}, err
	}

	payload := &genskills.LinkPayload{
		ID:               input.SkillID,
		TargetKind:       string(target.Kind),
		McpServerID:      nil,
		MetaMcpServerID:  nil,
		PinnedVersionID:  optionalString(pinned),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	}
	switch target.Kind {
	case SkillMCPTargetServer:
		payload.McpServerID = &target.ID
	case SkillMCPTargetGateway:
		payload.MetaMcpServerID = &target.ID
	case SkillMCPTargetOrganizationGateway:
	}
	written, err := s.skills.Link(ctx, payload)
	if err != nil {
		return LinkSkillToMCPOutput{}, err
	}

	// The receipt is the committed state read back, not the write's echo.
	confirmed, err := s.skills.ListLinks(ctx, skillLinkFilter(input.SkillID, target))
	if err != nil {
		return LinkSkillToMCPOutput{}, err
	}
	for _, link := range confirmed.Links {
		if link != nil && link.ID == written.ID {
			summary := buildSkillMCPLinkSummary(link)
			return LinkSkillToMCPOutput{
				ProjectSlug: project.Slug,
				Link:        summary,
				Message:     fmt.Sprintf("%s now serves the skill %q over MCP%s.", describeSkillMCPTarget(summary.Target), summary.SkillName, describeSkillPin(summary)),
			}, nil
		}
	}
	return LinkSkillToMCPOutput{}, ErrSkillLinkNotConfirmed
}

// UnlinkSkillFromMCPInput names one skill and the exact MCP surface that
// should stop serving it, on the same terms as LinkSkillToMCPInput.
type UnlinkSkillFromMCPInput struct {
	ProjectSlug    string
	SkillID        string
	TargetKind     string
	MCPServer      string
	Gateway        string
	IdempotencyKey string
}

// UnlinkSkillFromMCPOutput is the receipt for an unlink. Removed is false
// when the surface was not serving the skill to begin with.
type UnlinkSkillFromMCPOutput struct {
	ProjectSlug string         `json:"project_slug"`
	SkillID     string         `json:"skill_id"`
	SkillName   string         `json:"skill_name"`
	Target      SkillMCPTarget `json:"target"`
	Removed     bool           `json:"removed"`
	Message     string         `json:"message"`
}

// UnlinkSkillFromMCP stops one MCP surface serving a skill. A link that is
// already gone is a no-op, so the call is safe to retry; the post-write read
// confirms no live link to that surface remains.
func (s *SkillsService) UnlinkSkillFromMCP(ctx context.Context, principal Principal, input UnlinkSkillFromMCPInput) (UnlinkSkillFromMCPOutput, error) {
	ctx, project, err := s.begin(ctx, principal, input.ProjectSlug)
	if err != nil {
		return UnlinkSkillFromMCPOutput{}, err
	}
	if s.mcpTargets == nil {
		return UnlinkSkillFromMCPOutput{}, ErrSkillsUnavailable
	}
	if _, err := uuid.Parse(input.SkillID); err != nil {
		return UnlinkSkillFromMCPOutput{}, ErrRegistrationInvalid
	}
	if key := strings.TrimSpace(input.IdempotencyKey); key == "" || len(key) > maxSkillIdempotencyKeyLength {
		return UnlinkSkillFromMCPOutput{}, ErrRegistrationInvalid
	}

	// Reading the skill first names it in the receipt, refuses a skill this
	// project does not have as not_found, and refuses a caller without skill
	// access before any target is resolved.
	current, err := s.skills.Get(ctx, &genskills.GetPayload{
		ID:               input.SkillID,
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	if err != nil {
		return UnlinkSkillFromMCPOutput{}, err
	}
	target, err := s.resolveMCPTarget(ctx, principal, project, input.TargetKind, input.MCPServer, input.Gateway)
	if err != nil {
		return UnlinkSkillFromMCPOutput{}, err
	}

	existing, err := s.skills.ListLinks(ctx, skillLinkFilter(input.SkillID, target))
	if err != nil {
		return UnlinkSkillFromMCPOutput{}, err
	}
	removed := false
	for _, link := range existing.Links {
		if link == nil {
			continue
		}
		if err := s.skills.Unlink(ctx, &genskills.UnlinkPayload{
			LinkID:           link.ID,
			SessionToken:     nil,
			ApikeyToken:      nil,
			ProjectSlugInput: nil,
		}); err != nil {
			return UnlinkSkillFromMCPOutput{}, err
		}
		removed = true
	}

	remaining, err := s.skills.ListLinks(ctx, skillLinkFilter(input.SkillID, target))
	if err != nil {
		return UnlinkSkillFromMCPOutput{}, err
	}
	if len(remaining.Links) > 0 {
		return UnlinkSkillFromMCPOutput{}, ErrSkillLinkNotConfirmed
	}

	message := fmt.Sprintf("%s no longer serves the skill %q over MCP. The skill and its versions are unchanged.", describeSkillMCPTarget(target), current.Skill.Name)
	if !removed {
		message = fmt.Sprintf("%s was not serving the skill %q over MCP, so nothing changed.", describeSkillMCPTarget(target), current.Skill.Name)
	}
	return UnlinkSkillFromMCPOutput{
		ProjectSlug: project.Slug,
		SkillID:     current.Skill.ID,
		SkillName:   current.Skill.Name,
		Target:      target,
		Removed:     removed,
		Message:     message,
	}, nil
}

func describeSkillMCPTarget(target SkillMCPTarget) string {
	switch target.Kind {
	case SkillMCPTargetServer:
		return fmt.Sprintf("The MCP server %q", target.Name)
	case SkillMCPTargetGateway:
		return fmt.Sprintf("The gateway %q", target.Name)
	case SkillMCPTargetOrganizationGateway:
		return "The organization gateway"
	default:
		return "The MCP surface"
	}
}

func describeSkillPin(link SkillMCPLinkSummary) string {
	if link.FollowsLatest {
		return ", following its latest valid version"
	}
	return fmt.Sprintf(", pinned to version %s", link.PinnedVersionID)
}

// skillLinkRefusal maps the link-specific failures onto the structured codes
// the skills tools return. It reports false for anything else.
func skillLinkRefusal(err error) (skillsRefusalResult, bool) {
	switch {
	case errors.Is(err, skills.ErrSkillsOverMCPDisabled):
		return skillsRefusalResult{Code: unavailableCode, Message: "Serving skills over MCP is not switched on for your organization yet."}, true
	case errors.Is(err, ErrSkillMCPTargetNotFound):
		return skillsRefusalResult{Code: "not_found", Message: "No MCP server or gateway in this project has that exact ID, slug, or name. Name one that exists; nothing is picked by default."}, true
	case errors.Is(err, ErrSkillMCPTargetAmbiguous):
		return skillsRefusalResult{Code: "ambiguous_target", Message: "More than one MCP server or gateway in this project has that name. Name it by its ID instead."}, true
	case errors.Is(err, ErrSkillLinkNotConfirmed):
		return skillsRefusalResult{Code: "conflict", Message: "The change was sent, but reading it back did not show the expected state; another change may have landed at the same time. Read the skill's links again with list_skill_distributions before retrying."}, true
	default:
		return skillsRefusalResult{}, false
	}
}

// SkillMCPTargets lists the MCP servers and gateways a skill can be linked to
// in one project.
func (s *PostgresSkillTargets) SkillMCPTargets(ctx context.Context, organizationID string, projectID uuid.UUID, limit int) ([]SkillMCPTarget, error) {
	if s == nil || s.db == nil || organizationID == "" || projectID == uuid.Nil {
		return nil, ErrSkillsUnavailable
	}
	bounded := int32(min(max(limit, 1), maxSkillTargetLookup))
	rows, err := platformrepo.New(s.db).ListPlatformMCPProjectSkillLinkServers(ctx, platformrepo.ListPlatformMCPProjectSkillLinkServersParams{
		ProjectID:      projectID,
		OrganizationID: organizationID,
		ResultLimit:    bounded,
	})
	if err != nil {
		return nil, fmt.Errorf("list platform mcp skill link targets: %w", err)
	}
	targets := make([]SkillMCPTarget, 0, len(rows))
	for _, row := range rows {
		targets = append(targets, SkillMCPTarget{
			Kind: SkillMCPTargetKind(row.Kind),
			ID:   row.ID.String(),
			Name: row.Name,
			Slug: row.Slug,
		})
	}
	return targets, nil
}
