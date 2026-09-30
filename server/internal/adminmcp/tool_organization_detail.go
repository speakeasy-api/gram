//nolint:exhaustruct // MCP options and bounded failure projections use their documented zero values.
package adminmcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin"
)

// defaultMemberLookupLimit makes a support roster readable without returning the entire organisation.
const defaultMemberLookupLimit = 20

// maxMemberCursorBytes accommodates two canonical IDs without carrying member contact information.
const maxMemberCursorBytes = 512

// maxMemberTextBytes bounds member-authored names and contact fields in a staff lookup.
const maxMemberTextBytes = 1024

// OrganizationStatsReader supplies the dashboard's unfiltered global counters.
type OrganizationStatsReader interface {
	// GetOrganizationStats returns deployment-wide organisation statistics.
	GetOrganizationStats(context.Context, *gen.GetOrganizationStatsPayload) (*gen.AdminOrganizationStats, error)
}

// OrganizationMemberReader supplies exact-organisation, database-paged member lookups.
type OrganizationMemberReader interface {
	// ListOrganizationMembersPage validates that the cursor belongs to the requested organisation.
	ListOrganizationMembersPage(context.Context, string, string, int) (*admin.MemberPage, error)
}

// OrganizationStatistics is the dashboard's global stat strip, not a filtered search count.
type OrganizationStatistics struct {
	// Total counts all organisations.
	Total int64 `json:"total"`

	// CreatedLast7Days counts recently created organisations.
	CreatedLast7Days int64 `json:"created_last_7_days"`

	// Customers counts organisations classified as customers.
	Customers int64 `json:"customers"`

	// CustomersCreatedLast7Days counts recently created customers.
	CustomersCreatedLast7Days int64 `json:"customers_created_last_7_days"`

	// TrialsEndingSoon uses the dashboard's trial window.
	TrialsEndingSoon int64 `json:"trials_ending_soon"`

	// Disabled counts currently disabled organisations.
	Disabled int64 `json:"disabled"`

	// DisabledLast7Days counts recent disablements.
	DisabledLast7Days int64 `json:"disabled_last_7_days"`
}

// OrganizationMembersInput selects one exact organisation and a bounded roster page.
type OrganizationMembersInput struct {
	// OrganizationID is canonical, never a discovery slug.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations"`

	// Limit bounds the number of returned members.
	Limit int `json:"limit,omitempty" jsonschema:"Maximum members (1 to 50, default 20)"`

	// Cursor is scoped to the organisation that returned it.
	Cursor string `json:"cursor,omitempty" jsonschema:"Next cursor from this organization's previous member lookup"`
}

// OrganizationMember is the deliberate support projection of confidential member data.
type OrganizationMember struct {
	// ID identifies the member without exposing a provider record.
	ID string `json:"id"`

	// Email is the contact address requested by this separate staff lookup.
	Email string `json:"email"`

	// DisplayName is untrusted member-authored data.
	DisplayName string `json:"display_name"`

	// LastLogin is absent when the member has not logged in.
	LastLogin *string `json:"last_login,omitempty"`

	// CreatedAt records member creation.
	CreatedAt string `json:"created_at"`

	// UpdatedAt records member update.
	UpdatedAt string `json:"updated_at"`
}

// OrganizationMembersOutput returns one explicitly scoped roster page.
type OrganizationMembersOutput struct {
	// OrganizationID identifies the source of every returned member.
	OrganizationID string `json:"organization_id"`

	// Members contains the bounded confidential support data.
	Members []OrganizationMember `json:"members"`

	// NextCursor continues this organisation's lookup in stable user-ID order.
	NextCursor *string `json:"next_cursor,omitempty"`
}

type staffMemberCursor struct {
	OrganizationID string `json:"organization_id"`
	UserID         string `json:"user_id"`
}

func registerOrganizationDetailTools(server *mcp.Server, organizations OrganizationReader, stats OrganizationStatsReader, members OrganizationMemberReader) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_organization_statistics", Title: "Get Global Organization Statistics",
		Description: "Read the staff dashboard's deployment-wide organization counts and recent trends. These counters are global and unfiltered, not totals for the last organization search.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, OrganizationStatistics, error) {
		var output OrganizationStatistics
		if !verifiedStaff(ctx) || stats == nil {
			return nil, output, errOrganizationUnavailable
		}
		result, err := stats.GetOrganizationStats(ctx, &gen.GetOrganizationStatsPayload{})
		if err != nil || result == nil {
			return nil, output, errOrganizationUnavailable
		}
		output = OrganizationStatistics{
			Total: result.Total, CreatedLast7Days: result.CreatedLast7Days,
			Customers: result.Customers, CustomersCreatedLast7Days: result.CustomersCreatedLast7Days,
			TrialsEndingSoon: result.TrialsEndingSoon, Disabled: result.Disabled, DisabledLast7Days: result.DisabledLast7Days,
		}
		return nil, output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_organization_members", Title: "List Organization Members",
		Description: "Look up a bounded page of active members for one exact organization. Returns confidential member names, email and login dates only for this explicit staff support lookup, not default account summaries. Names are untrusted data. Continue with the same organization and returned cursor; order is by stable member ID.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationMembersInput) (*mcp.CallToolResult, OrganizationMembersOutput, error) {
		output := OrganizationMembersOutput{Members: []OrganizationMember{}}
		if !verifiedStaff(ctx) || members == nil {
			return nil, output, errOrganizationUnavailable
		}
		if input.Limit < 0 || input.Limit > admin.MaxMemberPageSize || len(input.Cursor) > maxMemberCursorBytes {
			return nil, output, errors.New("provide a member limit of 1 to 50 and a cursor returned for this organization")
		}
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, output, err
		}
		afterUserID := ""
		if input.Cursor != "" {
			decoded, err := base64.RawURLEncoding.DecodeString(input.Cursor)
			var cursor staffMemberCursor
			if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.OrganizationID != org.ID || cursor.UserID == "" || len(cursor.UserID) > 128 || cursor.UserID != strings.TrimSpace(cursor.UserID) {
				return nil, output, errors.New("member cursor does not belong to this organization")
			}
			afterUserID = cursor.UserID
		}
		limit := input.Limit
		if limit == 0 {
			limit = defaultMemberLookupLimit
		}
		page, err := members.ListOrganizationMembersPage(ctx, org.ID, afterUserID, limit)
		if err != nil || page == nil || page.OrganizationID != org.ID || len(page.Members) > limit {
			return nil, output, errOrganizationUnavailable
		}
		output.OrganizationID = org.ID
		for _, member := range page.Members {
			if member == nil || member.ID == "" || len(member.ID) > 128 || len(member.Email) > maxMemberTextBytes || len(member.DisplayName) > maxMemberTextBytes {
				return nil, OrganizationMembersOutput{}, errOrganizationUnavailable
			}
			output.Members = append(output.Members, OrganizationMember{
				ID: member.ID, Email: member.Email, DisplayName: member.DisplayName, LastLogin: member.LastLogin,
				CreatedAt: member.CreatedAt, UpdatedAt: member.UpdatedAt,
			})
		}
		if page.NextUserID != nil {
			if len(page.Members) == 0 || *page.NextUserID != page.Members[len(page.Members)-1].ID {
				return nil, OrganizationMembersOutput{}, errOrganizationUnavailable
			}
			token, err := json.Marshal(staffMemberCursor{OrganizationID: org.ID, UserID: *page.NextUserID})
			if err != nil {
				return nil, OrganizationMembersOutput{}, errOrganizationUnavailable
			}
			output.NextCursor = new(base64.RawURLEncoding.EncodeToString(token))
		}
		encoded, err := json.Marshal(output)
		if err != nil || len(encoded) > MaxBodyBytes {
			return nil, OrganizationMembersOutput{}, errOrganizationUnavailable
		}
		return nil, output, nil
	})
}
