// Consent-screen tool selection: resolves the toolset inventory the same way
// the runtime does, loads the subject's prefill, and parses the approve
// form's exclusive-mode picker fields into a resource-bound session policy
// validated against the challenge's cached inventory snapshot.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"

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

// consentToolNameLimit caps how many explicit tool names one approve form
// may submit. Matches consentInventoryMaxTools so any subset of a full
// inventory — including all of it — is submittable; the approve POST's 1 MiB
// body cap covers the worst case with URL-encoding inflation.
const consentToolNameLimit = consentInventoryMaxTools

// consentPrefillAttr serializes the subject's stored selection for the
// island's server-rendered bootstrap: annotation grants with their modes
// plus manually picked tool names. Snapshot expansions are omitted — the
// island re-previews annotation grants against the inventory it fetches.
// Empty when there is no restrictive prefill.
func consentPrefillAttr(prefill *toolfilter.SessionSelection) string {
	if prefill == nil {
		return ""
	}
	type prefillAnnotation struct {
		Name string `json:"name"`
		Mode string `json:"mode"`
	}
	annotations := []prefillAnnotation{}
	tools := []string{}
	for _, entry := range prefill.Allow {
		switch entry.Type {
		case toolfilter.AllowTypeAnnotation:
			if entry.Mode != nil {
				annotations = append(annotations, prefillAnnotation{Name: entry.Name, Mode: string(*entry.Mode)})
			}
		case toolfilter.AllowTypeTool:
			tools = append(tools, entry.Name)
		}
	}
	encoded, err := json.Marshal(map[string]any{"annotations": annotations, "tools": tools})
	if err != nil {
		return ""
	}
	return string(encoded)
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

func (s *Service) consentToolPickerEligible(ctx context.Context, endpoint *ResolvedMcpEndpoint) (bool, error) {
	if !endpoint.McpServerID.Valid {
		return false, nil
	}
	if !endpoint.ToolsetID.Valid {
		return true, nil
	}

	hasProxy, err := toolsets_repo.New(s.db).ToolsetHasExternalMCPProxy(ctx, toolsets_repo.ToolsetHasExternalMCPProxyParams{
		ToolsetID: endpoint.ToolsetID.UUID,
		ProjectID: endpoint.ProjectID,
	})
	if err != nil {
		return false, fmt.Errorf("check toolset for external MCP proxy: %w", err)
	}
	return !hasProxy, nil
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
		OrganizationID:      endpoint.OrganizationID,
		UserSessionIssuerID: endpoint.UserSessionIssuerID,
		UserSessionClientID: uuid.NullUUID{UUID: clientRowID, Valid: true},
		SubjectUrn:          subject,
		Resource:            resource,
	})
	if err != nil {
		return nil
	}
	sel, err := toolfilter.ParseSessionSelection(raw)
	if err != nil {
		s.logger.WarnContext(ctx, "ignore malformed stored tool selection for consent prefill", attr.SlogError(err))
		return nil
	}
	if sel == nil || sel.Resource != resource {
		return nil
	}
	return sel
}

// chosenToolSelection parses the approve form's tool picker against the
// challenge's cached inventory snapshot into a resource-bound policy. A
// missing tool_filtering field reads as "off" only when no selection fields
// accompany it: forms rendered before the picker deployed (the challenge
// TTL spans a rolling deploy) omit everything, and omission grants nothing
// the default "All tools" radio wouldn't.
//
// The island submits its grant intent: tool_annotations (snapshot-mode
// grants), tool_annotations_live (live-mode grants), and tools
// (individually picked names). Snapshot expansions are derived HERE from the display-bound
// inventory, never accepted from the form; unknown submitted tool names are
// intersected away (a crafted form can only narrow); manual picks already
// covered by a granted annotation are canonicalized away. Every granted
// annotation must match at least one displayed tool — a zero-match grant is
// a latent rule the user never saw and is rejected. An empty grant set
// persists a selection that authorizes zero tools.
//
// inventory is nil exactly when tool_filtering is off or absent — the caller
// loads a snapshot only for restrictive approvals — so the "on" branch fails
// closed if it is missing.
func chosenToolSelection(form url.Values, inventory *consentToolInventory) (*toolfilter.SessionSelection, error) {
	hasSelectionFields := len(form["tools"]) > 0 || len(form["tool_annotations"]) > 0 || len(form["tool_annotations_live"]) > 0

	switch form.Get("tool_filtering") {
	case "":
		if hasSelectionFields {
			return nil, fmt.Errorf("tool_filtering is required when selection fields are present")
		}
		return nil, nil
	case "off":
		if hasSelectionFields {
			return nil, fmt.Errorf(`tool_filtering "off" must not carry selection fields`)
		}
		return nil, nil
	case "on":
		if inventory == nil {
			return nil, fmt.Errorf("tool filtering requires a bound inventory snapshot")
		}
	default:
		return nil, fmt.Errorf(`tool_filtering must be "on" or "off"`)
	}

	// Annotation grants: vocabulary-checked, no duplicates within or across
	// the two mode fields. Live-vs-snapshot is the consenting subject's own
	// choice — there is no server-side gate — and every granted annotation
	// must match at least one displayed tool below.
	grantModes := map[string]toolfilter.AnnotationMode{}
	for _, name := range form["tool_annotations"] {
		if !slices.Contains(toolfilter.KnownAnnotations, name) {
			return nil, fmt.Errorf("unknown tool annotation %q", name)
		}
		if _, dup := grantModes[name]; dup {
			return nil, fmt.Errorf("annotation %q granted more than once", name)
		}
		grantModes[name] = toolfilter.AnnotationModeSnapshot
	}
	for _, name := range form["tool_annotations_live"] {
		if !slices.Contains(toolfilter.KnownAnnotations, name) {
			return nil, fmt.Errorf("unknown tool annotation %q", name)
		}
		if _, dup := grantModes[name]; dup {
			return nil, fmt.Errorf("annotation %q granted more than once", name)
		}
		grantModes[name] = toolfilter.AnnotationModeLive
	}

	// Per-annotation expansions against the displayed snapshot. A granted
	// annotation matching nothing displayed is rejected — the island never
	// offers it, so it marks a crafted form.
	expansions := map[string][]string{}
	matched := map[string]bool{}
	coveredByGrant := map[string]bool{}
	for _, tool := range inventory.Tools {
		for _, value := range tool.Annotations {
			mode, granted := grantModes[value]
			if !granted {
				continue
			}
			matched[value] = true
			coveredByGrant[tool.Name] = true
			if mode == toolfilter.AnnotationModeSnapshot {
				expansions[value] = append(expansions[value], tool.Name)
			}
		}
	}
	for name := range grantModes {
		if !matched[name] {
			return nil, fmt.Errorf("annotation %q matches no displayed tool", name)
		}
	}

	// Manual picks: strict duplicates, per-name cap, unknown intersected
	// away, grant-covered picks canonicalized away.
	submitted := form["tools"]
	if len(submitted) > consentToolNameLimit {
		return nil, fmt.Errorf("too many tools submitted (%d; limit is %d)", len(submitted), consentToolNameLimit)
	}
	displayed := make(map[string]bool, len(inventory.Tools))
	for _, tool := range inventory.Tools {
		displayed[tool.Name] = true
	}
	seen := map[string]bool{}
	manual := []string{}
	for _, name := range submitted {
		if len(name) > consentInventoryMaxNameBytes {
			return nil, fmt.Errorf("tool name exceeds %d bytes", consentInventoryMaxNameBytes)
		}
		if seen[name] {
			return nil, fmt.Errorf("tool %q picked more than once", name)
		}
		seen[name] = true
		if !displayed[name] || coveredByGrant[name] {
			continue
		}
		manual = append(manual, name)
	}

	entries := make([]toolfilter.AllowEntry, 0, len(grantModes)+len(manual))
	for name, mode := range grantModes {
		entry := toolfilter.AllowEntry{Type: toolfilter.AllowTypeAnnotation, Name: name, Mode: &mode, Tools: nil}
		if mode == toolfilter.AnnotationModeSnapshot {
			expansion := expansions[name]
			if expansion == nil {
				expansion = []string{}
			}
			entry.Tools = &expansion
		}
		entries = append(entries, entry)
	}
	for _, name := range manual {
		entries = append(entries, toolfilter.AllowEntry{Type: toolfilter.AllowTypeTool, Name: name, Mode: nil, Tools: nil})
	}

	selection, err := toolfilter.NewSessionSelection(inventory.Resource, uuid.New(), entries)
	if err != nil {
		return nil, fmt.Errorf("assemble tool selection: %w", err)
	}
	return selection, nil
}
