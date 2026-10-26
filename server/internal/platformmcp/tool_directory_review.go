//nolint:exhaustruct // MCP manifests and generated payloads use optional zero values.
package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

const (
	operationListDirectoryReviewItems   = "list_directory_review_items"
	operationResolveDirectoryReviewItem = "resolve_directory_review_item"
)

// DirectoryReviewBackend is the access service's review API. The tools call
// it under the caller's own auth context, so the live org:admin check, the
// transaction-time re-reads, version checks and audit are the dashboard's.
type DirectoryReviewBackend interface {
	ListDirectoryReviewItems(context.Context, *accessgen.ListDirectoryReviewItemsPayload) (*accessgen.ListDirectoryReviewItemsResult, error)
	ResolveDirectoryReviewItem(context.Context, *accessgen.ResolveDirectoryReviewItemPayload) (*accessgen.ResolveDirectoryReviewItemResult, error)
}

type DirectoryReviewSource struct {
	Kind             string `json:"kind" jsonschema:"group or attribute"`
	Name             string `json:"name" jsonschema:"the group name, or key=value for an attribute value"`
	DirectoryGroupID string `json:"directory_group_id,omitempty"`
	AttributeKey     string `json:"attribute_key,omitempty"`
	AttributeValue   string `json:"attribute_value,omitempty"`
}

type DirectoryReviewProposedRole struct {
	Action  string `json:"action" jsonschema:"reuse an existing role or create a new one"`
	Name    string `json:"name"`
	RoleURN string `json:"role_urn,omitempty"`
}

type DirectoryReviewItemSummary struct {
	ItemID           string                        `json:"item_id"`
	Kind             string                        `json:"kind" jsonschema:"new_group, new_attribute_value or source_missing"`
	Occurrence       int32                         `json:"occurrence"`
	RaisedAt         string                        `json:"raised_at"`
	Source           DirectoryReviewSource         `json:"source"`
	DirectoryPeople  int64                         `json:"directory_people" jsonschema:"live directory profiles matching the source; not organization members"`
	ExistingRoleURNs []string                      `json:"existing_role_urns"`
	ProposedRoles    []DirectoryReviewProposedRole `json:"proposed_roles"`
	Version          string                        `json:"version" jsonschema:"send back with resolve_directory_review_item to approve"`
}

type ListDirectoryReviewItemsOutput struct {
	Items []DirectoryReviewItemSummary `json:"items"`
}

type DirectoryReviewRoleChoice struct {
	RoleURN    string `json:"role_urn,omitempty" jsonschema:"principal URN of an existing role, from proposed_roles or list_access_roles"`
	CreateName string `json:"create_name,omitempty" jsonschema:"name of a new role to create with no grants"`
}

type ResolveDirectoryReviewItemInput struct {
	ItemID    string                      `json:"item_id" jsonschema:"exact item_id returned by list_directory_review_items"`
	Decision  string                      `json:"decision" jsonschema:"approve, dismiss, or opt_out_key (new attribute values only)"`
	Version   string                      `json:"version,omitempty" jsonschema:"the item's version from list_directory_review_items; required to approve"`
	Roles     []DirectoryReviewRoleChoice `json:"roles,omitempty" jsonschema:"role set to map a new source to, each with exactly one of role_urn or create_name; required to approve a new source and omitted otherwise"`
	Confirmed bool                        `json:"confirmed" jsonschema:"true only after the user confirms this exact item, decision and role set"`
}

type DirectoryReviewMappedRole struct {
	RoleURN string `json:"role_urn"`
}

type DirectoryReviewCreatedRole struct {
	RoleURN string `json:"role_urn"`
	Name    string `json:"name"`
}

type ResolveDirectoryReviewItemOutput struct {
	ItemID          string                       `json:"item_id"`
	State           string                       `json:"state" jsonschema:"approved or dismissed"`
	CreatedRoles    []DirectoryReviewCreatedRole `json:"created_roles"`
	MappedRoles     []DirectoryReviewMappedRole  `json:"mapped_roles"`
	RequestedWork   []string                     `json:"requested_work" jsonschema:"follow-up work requested after commit, never reported as done"`
	PendingItems    *int                         `json:"pending_items,omitempty" jsonschema:"items still pending after this decision, from a fresh read; absent when that read failed"`
	NextSafeActions string                       `json:"next_safe_actions"`
}

type directoryReviewRefusal struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// directoryReviewError is a refusal the tool reports before the backend runs.
type directoryReviewError struct {
	refusal directoryReviewRefusal
}

func (e *directoryReviewError) Error() string { return e.refusal.Message }

// WithDirectoryReview composes the review tools over the access service.
func (s *AccessReadService) WithDirectoryReview(backend DirectoryReviewBackend) *AccessReadService {
	if s != nil {
		s.directoryReview = backend
	}
	return s
}

func registerDirectoryReviewTools(reg *Registrar, accessReads *AccessReadService) {
	var backend DirectoryReviewBackend
	if accessReads != nil {
		backend = accessReads.directoryReview
	}
	meta := ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}

	addTool(reg, &mcp.Tool{
		Name:        operationListDirectoryReviewItems,
		Title:       "List Directory Changes to Review",
		Description: "List the directory changes waiting for an organization admin's decision, oldest first: a directory group first seen after review started with no role mapped, a new value of an attribute key that already has mappings, and a mapped group or attribute value that has gone from the directory. Each item carries the source, how many directory profiles match it (directory people, not organization members), the roles it maps to now, a proposed role set, and a version for resolve_directory_review_item. Pending items grant nothing. Nobody in the directory is listed.",
		Annotations: readOnlyAnnotations(),
	}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListDirectoryReviewItemsOutput, error) {
		return principalToolCall(ctx, directoryReviewToolResult, func(principal Principal) (ListDirectoryReviewItemsOutput, error) {
			if backend == nil {
				return ListDirectoryReviewItemsOutput{}, directoryReviewUnavailable()
			}
			listed, err := backend.ListDirectoryReviewItems(directoryReviewContext(ctx, principal), &accessgen.ListDirectoryReviewItemsPayload{})
			if err != nil {
				return ListDirectoryReviewItemsOutput{}, fmt.Errorf("list directory review items: %w", err)
			}
			return directoryReviewItems(listed), nil
		})
	})

	addTool(reg, &mcp.Tool{
		Name:  operationResolveDirectoryReviewItem,
		Title: "Decide One Directory Change",
		Description: "Decide one pending item from list_directory_review_items, through the same checks as the dashboard. approve on a new group or attribute value maps it to the given role set, reusing existing roles by role_urn or creating new roles with no grants by create_name; approve needs the item's current version and fails with conflict if the source or its mappings changed. approve on a missing source removes its role mappings, so its people lose those roles. dismiss leaves a new source unmapped, or keeps a missing source's mappings. opt_out_key dismisses a new attribute value and stops raising new values of its key. " +
			"First show the user the exact source, decision and role set, and retry with confirmed: true once they agree. An item that is no longer pending returns conflict, so repeating a decision never applies it twice. The result reports committed state and a fresh pending count; WorkOS role sync and role distribution are requested, not verified.",
		// WorkOS role sync is requested in the background, not called here.
		Annotations: writeAnnotations(true, true, false),
	}, meta, func(ctx context.Context, _ *mcp.CallToolRequest, input ResolveDirectoryReviewItemInput) (*mcp.CallToolResult, ResolveDirectoryReviewItemOutput, error) {
		return principalToolCall(ctx, directoryReviewToolResult, func(principal Principal) (ResolveDirectoryReviewItemOutput, error) {
			if backend == nil {
				return ResolveDirectoryReviewItemOutput{}, directoryReviewUnavailable()
			}
			return resolveDirectoryReviewItem(directoryReviewContext(ctx, principal), backend, input)
		})
	})
}

func resolveDirectoryReviewItem(ctx context.Context, backend DirectoryReviewBackend, input ResolveDirectoryReviewItemInput) (ResolveDirectoryReviewItemOutput, error) {
	if !input.Confirmed {
		return ResolveDirectoryReviewItemOutput{}, &directoryReviewError{directoryReviewRefusal{
			Code:    "confirmation_required",
			Message: "Show the user the exact source, decision and role set, ask them to confirm, then retry with confirmed: true.",
		}}
	}
	itemID := strings.TrimSpace(input.ItemID)
	payload := &accessgen.ResolveDirectoryReviewItemPayload{ID: itemID, Decision: strings.TrimSpace(input.Decision)}
	if version := strings.TrimSpace(input.Version); version != "" {
		payload.Version = &version
	}
	for _, role := range input.Roles {
		choice := &accessgen.ApplyDirectoryRoleSetupRole{}
		if urn := strings.TrimSpace(role.RoleURN); urn != "" {
			choice.RoleUrn = &urn
		}
		if name := strings.TrimSpace(role.CreateName); name != "" {
			choice.CreateName = &name
		}
		payload.Roles = append(payload.Roles, choice)
	}

	resolved, err := backend.ResolveDirectoryReviewItem(ctx, payload)
	if err != nil {
		return ResolveDirectoryReviewItemOutput{}, fmt.Errorf("resolve directory review item: %w", err)
	}

	output := ResolveDirectoryReviewItemOutput{
		ItemID: itemID, State: resolved.State,
		CreatedRoles:  make([]DirectoryReviewCreatedRole, 0, len(resolved.CreatedRoles)),
		MappedRoles:   make([]DirectoryReviewMappedRole, 0, len(resolved.Mappings)),
		RequestedWork: make([]string, 0, len(resolved.SideEffects)),
	}
	for _, role := range resolved.CreatedRoles {
		if role != nil {
			output.CreatedRoles = append(output.CreatedRoles, DirectoryReviewCreatedRole{RoleURN: role.PrincipalUrn, Name: role.Name})
		}
	}
	for _, mapping := range resolved.Mappings {
		if mapping != nil {
			output.MappedRoles = append(output.MappedRoles, DirectoryReviewMappedRole{RoleURN: mapping.RoleUrn})
		}
	}
	for _, effect := range resolved.SideEffects {
		if effect != nil {
			output.RequestedWork = append(output.RequestedWork, effect.Kind)
		}
	}

	// A fresh read shows what is left to decide. The decision has committed
	// either way, so a failed read is reported, not returned as an error.
	output.NextSafeActions = "Call list_directory_review_items for the remaining items."
	if listed, err := backend.ListDirectoryReviewItems(ctx, &accessgen.ListDirectoryReviewItemsPayload{}); err == nil && listed != nil {
		pending := len(listed.Items)
		output.PendingItems = &pending
	} else {
		output.NextSafeActions = "The decision was saved, but the remaining items could not be read. " + output.NextSafeActions
	}
	if len(output.CreatedRoles) > 0 {
		output.NextSafeActions = "New roles grant nothing until permissions are added to them. " + output.NextSafeActions
	}
	return output, nil
}

// directoryReviewContext acts as the calling admin, so the access service
// authorizes, re-reads and audits as it does for the dashboard. It is built
// from the principal alone: no support-session or other provenance carries
// over, so the service checks the caller's live org admin membership. Only
// display fields are taken from the caller's own context.
func directoryReviewContext(ctx context.Context, principal Principal) context.Context {
	ac := &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}
	if current, ok := contextvalues.GetAuthContext(ctx); ok && current != nil && current.UserID == principal.UserID && current.ActiveOrganizationID == principal.OrganizationID {
		ac.OrganizationSlug, ac.Email = current.OrganizationSlug, current.Email
	}
	return contextvalues.SetAuthContext(ctx, ac)
}

func directoryReviewItems(listed *accessgen.ListDirectoryReviewItemsResult) ListDirectoryReviewItemsOutput {
	output := ListDirectoryReviewItemsOutput{Items: []DirectoryReviewItemSummary{}}
	if listed == nil {
		return output
	}
	for _, item := range listed.Items {
		if item == nil || item.Item == nil || item.Item.Source == nil {
			continue
		}
		preview := item.Item
		source := DirectoryReviewSource{Kind: preview.Source.SourceKind, Name: preview.SourceName}
		if preview.Source.DirectoryGroupID != nil {
			source.DirectoryGroupID = *preview.Source.DirectoryGroupID
		}
		if preview.Source.AttributeKey != nil {
			source.AttributeKey = *preview.Source.AttributeKey
		}
		if preview.Source.AttributeValue != nil {
			source.AttributeValue = *preview.Source.AttributeValue
		}
		summary := DirectoryReviewItemSummary{
			ItemID: item.ID, Kind: item.Kind, Occurrence: item.Occurrence, RaisedAt: item.CreatedAt,
			Source: source, DirectoryPeople: preview.DirectoryPeople,
			ExistingRoleURNs: append([]string{}, preview.ExistingRoleUrns...),
			ProposedRoles:    make([]DirectoryReviewProposedRole, 0, len(preview.ProposedRoles)),
			Version:          preview.Version,
		}
		for _, role := range preview.ProposedRoles {
			if role == nil {
				continue
			}
			proposed := DirectoryReviewProposedRole{Action: role.RoleAction, Name: role.RoleName}
			if role.RoleUrn != nil {
				proposed.RoleURN = *role.RoleUrn
			}
			summary.ProposedRoles = append(summary.ProposedRoles, proposed)
		}
		output.Items = append(output.Items, summary)
	}
	return output
}

func directoryReviewUnavailable() error {
	return &directoryReviewError{directoryReviewRefusal{Code: unavailableCode, Message: "Directory review is unavailable on this server."}}
}

func directoryReviewToolResult(err error) (*mcp.CallToolResult, bool) {
	var refusal directoryReviewRefusal
	if reviewErr, ok := errors.AsType[*directoryReviewError](err); ok {
		refusal = reviewErr.refusal
	} else if shareable, ok := errors.AsType[*oops.ShareableError](err); ok {
		switch shareable.Code {
		case oops.CodeForbidden, oops.CodeUnauthorized:
			refusal = directoryReviewRefusal{Code: "permission_denied", Message: "Directory review requires organization administration."}
		case oops.CodeNotFound:
			refusal = directoryReviewRefusal{Code: "not_found", Message: "That review item does not exist in this organization. List the items again and use an item_id from the new result."}
		case oops.CodeConflict:
			refusal = directoryReviewRefusal{Code: "conflict", Message: "The item was already decided, or its source or mappings changed. List the items again and decide from the new result."}
		case oops.CodeBadRequest:
			refusal = directoryReviewRefusal{Code: "invalid_request", Message: shareable.Error()}
		default:
			return nil, false
		}
	} else {
		return nil, false
	}
	content, marshalErr := json.Marshal(refusal)
	if marshalErr != nil {
		return nil, false
	}
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: string(content)}}}, true
}
