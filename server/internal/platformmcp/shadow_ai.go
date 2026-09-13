package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/agent/aitargets"
	agentrepo "github.com/speakeasy-api/gram/server/internal/agent/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

var ErrShadowAIUnavailable = errors.New("platform mcp shadow ai unavailable")

const shadowAIFeature = "shadow_ai"

// shadowAIDetectionReader is the access service's trusted-caller seam for the
// Shadow AI inventory.
type shadowAIDetectionReader interface {
	ReadAIDetections(context.Context, access.AIDetectionsReadInput) (*accessgen.ListAIDetectionsResult, error)
}

// shadowAILibraryReader loads the organization's served scan-target library:
// Speakeasy's built-ins overlaid with whatever the organization changed.
type shadowAILibraryReader interface {
	LoadLibrary(context.Context, string) (*aitargets.OrganizationList, error)
}

// PostgresShadowAILibrary reads the library from the agent tables, where
// Overlay composes it per read — no snapshot to go stale against the defaults.
type PostgresShadowAILibrary struct {
	queries *agentrepo.Queries
}

func NewPostgresShadowAILibrary(queries *agentrepo.Queries) *PostgresShadowAILibrary {
	return &PostgresShadowAILibrary{queries: queries}
}

func (l *PostgresShadowAILibrary) LoadLibrary(ctx context.Context, organizationID string) (*aitargets.OrganizationList, error) {
	if l == nil || l.queries == nil {
		return nil, ErrShadowAIUnavailable
	}
	list, err := aitargets.LoadOrganizationList(ctx, l.queries, organizationID)
	if err != nil {
		return nil, fmt.Errorf("load ai scan library: %w", err)
	}
	return list, nil
}

// ShadowAIService serves the two Shadow AI reads: what enrolled devices are
// running, and what the agents probe for. Organization-scoped, because a
// detection belongs to a device and a person rather than to a project.
type ShadowAIService struct {
	detections shadowAIDetectionReader
	library    shadowAILibraryReader
	authorizer Authorizer
	budget     OperationBudget
}

func NewShadowAIService(detections shadowAIDetectionReader, library shadowAILibraryReader, authorizer Authorizer, budget OperationBudget) *ShadowAIService {
	if detections == nil || library == nil || authorizer == nil || !budget.valid() {
		return nil
	}
	return &ShadowAIService{detections: detections, library: library, authorizer: authorizer, budget: budget}
}

func (s *ShadowAIService) valid() bool {
	return s != nil && s.detections != nil && s.library != nil && s.authorizer != nil && s.budget.valid()
}

// admit rechecks org:admin live rather than trusting the session that
// installed the package: the dashboard withholds user and device counts below
// that grant, and this surface must not be the way around it.
func (s *ShadowAIService) admit(ctx context.Context, principal Principal) error {
	if !s.valid() || principal.OrganizationID == "" {
		return ErrShadowAIUnavailable
	}
	if err := s.authorizer.RequireLiveOrgAdmin(ctx, principal); err != nil {
		return fmt.Errorf("require live org admin for shadow ai: %w", err)
	}
	return s.budget.Allow(ctx, principal)
}

type ListShadowAIToolsInput struct {
	Category string `json:"category,omitempty" jsonschema:"narrow to one kind of tool: harness, assistant or local_model; omit for all three"`
}

type ShadowAIToolSummary struct {
	TargetID    string `json:"target_id"`
	DisplayName string `json:"display_name"`
	Category    string `json:"category"`
	// State is the enforcement verdict: allowed, blocked or unreviewed. A
	// tool Gram cannot recognize at the gateway always reads unreviewed.
	State string `json:"state"`
	// Enforceable reports whether a decision about this tool could reach the
	// gateway at all, which requires it to publish a client ID metadata
	// document. Explains why a tool cannot leave unreviewed.
	Enforceable bool     `json:"enforceable"`
	Signals     []string `json:"signals"`
	UserCount   int64    `json:"user_count"`
	DeviceCount int64    `json:"device_count"`
	LastSeen    string   `json:"last_seen"`
}

type ListShadowAIToolsOutput struct {
	Tools []ShadowAIToolSummary `json:"tools"`
}

func (s *ShadowAIService) ListTools(ctx context.Context, principal Principal, input ListShadowAIToolsInput) (ListShadowAIToolsOutput, error) {
	if err := s.admit(ctx, principal); err != nil {
		return ListShadowAIToolsOutput{}, err
	}
	result, err := s.detections.ReadAIDetections(ctx, access.AIDetectionsReadInput{
		OrganizationID: principal.OrganizationID,
		Category:       strings.TrimSpace(input.Category),
		// admit cleared the live org:admin check, the same grant the
		// dashboard requires to see who and how many.
		Attributed: true,
	})
	if err != nil {
		return ListShadowAIToolsOutput{}, fmt.Errorf("read shadow ai detections: %w", err)
	}
	tools := make([]ShadowAIToolSummary, 0, len(result.Detections))
	for _, detection := range result.Detections {
		if detection == nil {
			continue
		}
		summary := ShadowAIToolSummary{
			TargetID:    detection.TargetID,
			DisplayName: detection.DisplayName,
			Category:    detection.Category,
			State:       "",
			Enforceable: false,
			Signals:     detection.Signals,
			UserCount:   conv.PtrValOr(detection.UserCount, 0),
			DeviceCount: conv.PtrValOr(detection.DeviceCount, 0),
			LastSeen:    detection.LastSeen,
		}
		if detection.Access != nil {
			summary.State = detection.Access.State
			summary.Enforceable = detection.Access.Enforceable
		}
		tools = append(tools, summary)
	}
	return ListShadowAIToolsOutput{Tools: tools}, nil
}

type ListAIScanLibraryInput struct {
	Category string `json:"category,omitempty" jsonschema:"narrow to one kind of target: harness, assistant or local_model; omit for all three"`
}

type AIScanTargetSummary struct {
	TargetID    string `json:"target_id"`
	DisplayName string `json:"display_name"`
	Category    string `json:"category"`
	// Origin is default for a target Speakeasy ships or organization for one
	// this organization added. A default can only be switched on or off.
	Origin string `json:"origin"`
	// Enabled reports whether agents are currently served this target.
	Enabled bool `json:"enabled"`
	// Blockable reports whether the target publishes a client ID metadata
	// document, which is the only thing a gateway block can be enforced
	// through.
	Blockable bool `json:"blockable"`
}

type ListAIScanLibraryOutput struct {
	// LibraryVersion is what agents echo back once they have this list.
	LibraryVersion int64                 `json:"library_version"`
	Targets        []AIScanTargetSummary `json:"targets"`
}

func (s *ShadowAIService) ListLibrary(ctx context.Context, principal Principal, input ListAIScanLibraryInput) (ListAIScanLibraryOutput, error) {
	if err := s.admit(ctx, principal); err != nil {
		return ListAIScanLibraryOutput{}, err
	}
	category := strings.TrimSpace(input.Category)
	if category != "" && !slices.Contains(aitargets.KnownCategories(), aitargets.Category(category)) {
		return ListAIScanLibraryOutput{}, ErrShadowAIUnavailable
	}
	list, err := s.library.LoadLibrary(ctx, principal.OrganizationID)
	if err != nil {
		return ListAIScanLibraryOutput{}, fmt.Errorf("read ai scan library: %w", err)
	}
	targets := make([]AIScanTargetSummary, 0, len(list.Entries))
	for _, entry := range list.Entries {
		if category != "" && string(entry.Category) != category {
			continue
		}
		targets = append(targets, AIScanTargetSummary{
			TargetID:    entry.ID,
			DisplayName: entry.DisplayName,
			Category:    string(entry.Category),
			Origin:      string(entry.Source),
			Enabled:     entry.Enabled,
			Blockable:   aitargets.Enforceable(entry.Target),
		})
	}
	return ListAIScanLibraryOutput{LibraryVersion: int64(list.Snapshot.ListVersion), Targets: targets}, nil
}
