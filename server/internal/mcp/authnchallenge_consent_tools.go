// Consent-screen tool picker: builds the view model for the "Tool access"
// section and parses the approve form's selection into a resource-bound
// session policy. Only toolset-backed endpoints offer the picker; proxy
// (external MCP) tools are listed nowhere because restrictive selections
// exclude them wholesale in v1.

package mcp

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcp/toolfilter"
	mcpservers_repo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	toolsets_repo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// consentToolNameLimit caps how many explicit tool names one selection may
// carry; combined with the per-name length cap it keeps a legitimate form
// well under the 64 KiB body limit.
const (
	consentToolNameLimit    = 256
	consentToolNameMaxRunes = 200
)

// consentAnnotationLabels maps the fixed annotation vocabulary to its
// user-facing labels, in display order (toolfilter.KnownAnnotations).
var consentAnnotationLabels = map[string]string{
	toolfilter.AnnotationReadOnly:    "Read-only",
	toolfilter.AnnotationDestructive: "Destructive",
	toolfilter.AnnotationIdempotent:  "Idempotent",
	toolfilter.AnnotationOpenWorld:   "Open world",
}

// consentAnnotationOption is one annotation checkbox.
type consentAnnotationOption struct {
	Value   string
	Label   string
	Count   int
	Checked bool
}

// consentToolEntry is one tool checkbox row.
type consentToolEntry struct {
	Name string
	// Annotations are the raw hints that are explicitly true, as vocabulary
	// values — rendered as badges and mirrored into a data attribute so the
	// script can mark rows covered by a selected annotation.
	Annotations       []string
	AnnotationsJoined string
	Checked           bool
}

// consentToolScope is a tag-scope convenience chip: clicking it checks its
// member tools. Selection stays name-based (snapshot), so the chip is pure
// UI sugar.
type consentToolScope struct {
	Tag         string
	Count       int
	NamesJoined string
}

// consentToolsSection is the template view model for the tool picker.
type consentToolsSection struct {
	Supported bool
	// FilteringEnabled preselects "Limit tools" when a live session for the
	// same subject/client/resource already carries a restrictive policy.
	FilteringEnabled   bool
	AnnotationOptions  []consentAnnotationOption
	Tools              []consentToolEntry
	Scopes             []consentToolScope
	UnannotatedCount   int
	ProxyExcludedCount int
}

// describeConsentToolset resolves the endpoint's toolset and effective
// variation group exactly the way the runtime does, so the picker shows the
// same inventory tools/list will serve.
func (s *Service) describeConsentToolset(ctx context.Context, endpoint *ResolvedMcpEndpoint) (*types.Toolset, error) {
	if !endpoint.ToolsetID.Valid {
		return nil, nil
	}

	toolsetRow, err := toolsets_repo.New(s.db).GetToolsetByIDAndProject(ctx, toolsets_repo.GetToolsetByIDAndProjectParams{
		ID:        endpoint.ToolsetID.UUID,
		ProjectID: endpoint.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("load toolset for consent picker: %w", err)
	}

	var serverGroupID *uuid.UUID
	if endpoint.McpServerID.Valid {
		serverRow, serr := mcpservers_repo.New(s.db).GetMCPServerByIDAndProjectID(ctx, mcpservers_repo.GetMCPServerByIDAndProjectIDParams{
			ID:        endpoint.McpServerID.UUID,
			ProjectID: endpoint.ProjectID,
		})
		if serr != nil {
			// Falling back to the toolset's group here would render (and
			// validate against) a different inventory than the runtime
			// resolves — fail instead.
			return nil, fmt.Errorf("load mcp server for consent picker: %w", serr)
		}
		if serverRow.ToolVariationsGroupID.Valid {
			id := serverRow.ToolVariationsGroupID.UUID
			serverGroupID = &id
		}
	}
	var toolsetGroupID *uuid.UUID
	if toolsetRow.ToolVariationsGroupID.Valid {
		id := toolsetRow.ToolVariationsGroupID.UUID
		toolsetGroupID = &id
	}
	groupID := toolfilter.ResolveGroupID(serverGroupID, toolsetGroupID)

	toolset, err := mv.DescribeToolset(ctx, s.logger, s.db, mv.ProjectID(endpoint.ProjectID), mv.ToolsetSlug(conv.ToLower(toolsetRow.Slug)), &s.toolsetCache, groupID, s.platformExtras...)
	if err != nil {
		return nil, err
	}
	return toolset, nil
}

// buildConsentToolsSection materializes the picker view model. prefill is
// the stored selection from the subject's newest live session (nil when
// none or not applicable); it must already be resource-checked.
func buildConsentToolsSection(toolset *types.Toolset, prefill *toolfilter.SessionSelection) consentToolsSection {
	section := consentToolsSection{
		Supported:          toolset != nil,
		FilteringEnabled:   prefill != nil,
		AnnotationOptions:  nil,
		Tools:              nil,
		Scopes:             nil,
		UnannotatedCount:   0,
		ProxyExcludedCount: 0,
	}
	if toolset == nil {
		return section
	}

	prefillAnnotations := map[string]bool{}
	prefillNames := map[string]bool{}
	if prefill != nil {
		for _, a := range prefill.Annotations {
			prefillAnnotations[a] = true
		}
		for _, name := range prefill.Tools {
			prefillNames[name] = true
		}
	}

	counts := map[string]int{}
	scopeNames := map[string][]string{}
	for _, tool := range toolset.Tools {
		if tool == nil {
			continue
		}
		if conv.IsProxyTool(tool) {
			section.ProxyExcludedCount++
			continue
		}
		base, err := conv.ToBaseTool(tool)
		if err != nil {
			continue
		}

		annotations := trueAnnotationValues(base.Annotations)
		if len(annotations) == 0 {
			section.UnannotatedCount++
		}
		for _, a := range annotations {
			counts[a]++
		}
		for _, tag := range toolfilter.EffectiveToolTags(tool) {
			scopeNames[tag] = append(scopeNames[tag], base.Name)
		}

		section.Tools = append(section.Tools, consentToolEntry{
			Name:              base.Name,
			Annotations:       annotations,
			AnnotationsJoined: strings.Join(annotations, " "),
			Checked:           prefillNames[base.Name],
		})
	}
	slices.SortFunc(section.Tools, func(a, b consentToolEntry) int {
		return strings.Compare(a.Name, b.Name)
	})

	for _, value := range toolfilter.KnownAnnotations {
		section.AnnotationOptions = append(section.AnnotationOptions, consentAnnotationOption{
			Value:   value,
			Label:   consentAnnotationLabels[value],
			Count:   counts[value],
			Checked: prefillAnnotations[value],
		})
	}

	for _, tag := range slices.Sorted(maps.Keys(scopeNames)) {
		names := scopeNames[tag]
		slices.Sort(names)
		section.Scopes = append(section.Scopes, consentToolScope{
			Tag:         tag,
			Count:       len(names),
			NamesJoined: strings.Join(names, ","),
		})
	}

	return section
}

// trueAnnotationValues lists the vocabulary values whose raw hint is
// explicitly true — the same axis the runtime matcher applies.
func trueAnnotationValues(annotations *types.ToolAnnotations) []string {
	if annotations == nil {
		return nil
	}
	var values []string
	hintTrue := func(hint *bool) bool { return hint != nil && *hint }
	if hintTrue(annotations.ReadOnlyHint) {
		values = append(values, toolfilter.AnnotationReadOnly)
	}
	if hintTrue(annotations.DestructiveHint) {
		values = append(values, toolfilter.AnnotationDestructive)
	}
	if hintTrue(annotations.IdempotentHint) {
		values = append(values, toolfilter.AnnotationIdempotent)
	}
	if hintTrue(annotations.OpenWorldHint) {
		values = append(values, toolfilter.AnnotationOpenWorld)
	}
	return values
}

// consentToolSelectionPrefill loads the stored selection from the subject's
// newest live session for the same issuer + client, returning nil unless it
// is restrictive AND bound to this endpoint's resource. Anonymous subjects
// never prefill: each authorization mints a fresh random subject, and
// matching by issuer/client alone would leak another user's preference.
func (s *Service) consentToolSelectionPrefill(ctx context.Context, endpoint *ResolvedMcpEndpoint, subject urn.SessionSubject, clientRowID uuid.UUID) *toolfilter.SessionSelection {
	if subject.Kind == urn.SessionSubjectKindAnonymous {
		return nil
	}
	resource := endpointToolSelectionResource(endpoint)
	if resource == "" {
		return nil
	}
	raw, err := usersessions_repo.New(s.db).GetLatestLiveUserSessionToolSelection(ctx, usersessions_repo.GetLatestLiveUserSessionToolSelectionParams{
		ProjectID:           endpoint.ProjectID,
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		UserSessionClientID: uuid.NullUUID{UUID: clientRowID, Valid: true},
		SubjectUrn:          subject,
		Resource:            resource,
	})
	if err != nil {
		return nil
	}
	sel, _, err := toolfilter.ParseSessionSelection(raw)
	if err != nil {
		s.logger.WarnContext(ctx, "ignore malformed stored tool selection for consent prefill", attr.SlogError(err))
		return nil
	}
	if sel == nil || sel.Resource != resource {
		return nil
	}
	return sel
}

// chosenToolSelection parses the approve form's tool picker into a
// resource-bound policy. Non-toolset endpoints never carry a policy. A
// missing tool_filtering field reads as "off": forms rendered before the
// picker deployed (the challenge TTL spans a rolling deploy) omit it, and
// omission grants nothing the default "All tools" radio wouldn't — only an
// unknown value marks a crafted form and fails the approve. Unknown
// submitted names are intersected away against the live inventory (a
// crafted form can only narrow), but an inventory resolution failure is an
// error: degrading to name-less would widen the selection.
func (s *Service) chosenToolSelection(ctx context.Context, endpoint *ResolvedMcpEndpoint, form url.Values) (*toolfilter.SessionSelection, error) {
	resource := endpointToolSelectionResource(endpoint)
	if !endpoint.ToolsetID.Valid || resource == "" {
		return nil, nil
	}
	switch form.Get("tool_filtering") {
	case "", "off":
		return nil, nil
	case "on":
		// fall through
	default:
		return nil, fmt.Errorf("tool_filtering must be \"on\" or \"off\"")
	}

	var annotations []string
	for _, a := range form["tool_annotations"] {
		if slices.Contains(toolfilter.KnownAnnotations, a) && !slices.Contains(annotations, a) {
			annotations = append(annotations, a)
		}
	}

	var names []string
	submitted := form["tools"]
	if len(submitted) > 0 {
		toolset, err := s.describeConsentToolset(ctx, endpoint)
		if err != nil {
			return nil, fmt.Errorf("resolve toolset for consent selection: %w", err)
		}
		live := map[string]bool{}
		if toolset != nil {
			for _, tool := range toolset.Tools {
				if tool == nil || conv.IsProxyTool(tool) {
					continue
				}
				if base, berr := conv.ToBaseTool(tool); berr == nil {
					live[base.Name] = true
				}
			}
		}
		seen := map[string]bool{}
		for _, name := range submitted {
			if len(names) >= consentToolNameLimit {
				break
			}
			if len([]rune(name)) > consentToolNameMaxRunes || seen[name] || !live[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}

	return &toolfilter.SessionSelection{
		Annotations: annotations,
		Tools:       names,
		Resource:    resource,
	}, nil
}
