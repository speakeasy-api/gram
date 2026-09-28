//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
)

var errCoverageUnavailable = errors.New("organization support coverage is unavailable")

// CoverageCell is one capability's evidence on one consuming surface.
type CoverageCell struct {
	Capability string `json:"capability"`
	Surface    string `json:"surface"`
	Status     string `json:"status"`
	Value      int64  `json:"value"`
	// Unit names what Value counts when the capability's own unit does not
	// apply, e.g. tool calls on the gateway where an agent surface counts
	// sessions.
	Unit     string  `json:"unit,omitempty"`
	Detail   string  `json:"detail,omitempty"`
	LastSeen *string `json:"last_seen,omitempty"`
}

// OrganizationCoverage is the observed coverage matrix for one organization.
type OrganizationCoverage struct {
	OrganizationID string         `json:"organization_id"`
	WindowDays     int            `json:"window_days"`
	From           string         `json:"from"`
	To             string         `json:"to"`
	Cells          []CoverageCell `json:"cells"`
	// Hook sources that folded onto no surface. Non-empty means the matrix is
	// not accounting for everything the organization did.
	UnmappedSources []string `json:"unmapped_sources,omitempty"`
}

func registerCoverageTools(server *mcp.Server, organizations OrganizationReader, coverage CoverageReader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_organization_support_coverage",
		Title:       "What Gram Observes for an Organization",
		Description: "Report which surfaces Gram has evidence for in an exact organization — its own MCP gateway plus the consuming agent surfaces (Claude Code, Claude Chat, Cowork, Codex, Cursor, other agents) — across session activity, policy enforcement, identity attribution, token usage and shadow MCP. A cell reporting no evidence means nothing was observed, not that the surface is unsupported; a cell reporting 'na' can never report for that pair.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input OrganizationIDInput) (*mcp.CallToolResult, OrganizationCoverage, error) {
		org, err := readExactOrganization(ctx, organizations, input.OrganizationID)
		if err != nil {
			return nil, OrganizationCoverage{}, err
		}
		if coverage == nil {
			return nil, OrganizationCoverage{}, errCoverageUnavailable
		}
		result, err := coverage.GetSupportCoverage(ctx, &gen.GetSupportCoveragePayload{OrganizationID: org.ID})
		if err != nil || result == nil {
			return nil, OrganizationCoverage{}, errCoverageUnavailable
		}

		cells := make([]CoverageCell, 0, len(result.Cells))
		for _, cell := range result.Cells {
			cells = append(cells, CoverageCell{
				Capability: cell.Capability,
				Surface:    cell.Surface,
				Status:     cell.Status,
				Value:      cell.Value,
				Unit:       cell.Unit,
				Detail:     cell.Detail,
				LastSeen:   cell.LastSeen,
			})
		}
		unmapped := make([]string, 0, len(result.Unmapped))
		for _, item := range result.Unmapped {
			unmapped = append(unmapped, item.HookSource)
		}

		return nil, OrganizationCoverage{
			OrganizationID:  org.ID,
			WindowDays:      result.WindowDays,
			From:            result.From,
			To:              result.To,
			Cells:           cells,
			UnmappedSources: unmapped,
		}, nil
	})
}
