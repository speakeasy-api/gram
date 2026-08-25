package platformmcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

var ErrInventoryCursorInvalid = errors.New("invalid platform MCP inventory cursor")

type inventoryCursor struct {
	OrganizationID string `json:"organization_id"`
	Binding        string `json:"binding"`
	ProjectID      string `json:"project_id"`
	Query          string `json:"query"`
	AfterMCPID     string `json:"after_mcp_id"`
}

type inventoryCursorCodec struct {
	key []byte
}

func newInventoryCursorCodec(keyMaterial string) (*inventoryCursorCodec, error) {
	if keyMaterial == "" {
		return nil, ErrInventoryCursorInvalid
	}
	key := sha256.Sum256([]byte("platform-mcp-inventory-cursor:" + keyMaterial))
	return &inventoryCursorCodec{key: key[:]}, nil
}

func (c *inventoryCursorCodec) Encode(cursor inventoryCursor) (string, error) {
	if c == nil || len(c.key) == 0 || cursor.OrganizationID == "" || cursor.Binding == "" || cursor.ProjectID == "" || cursor.AfterMCPID == "" || cursor.Query != "" {
		return "", ErrInventoryCursorInvalid
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode Platform MCP inventory cursor: %w", err)
	}
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	token := make([]byte, 0, len(payload)+sha256.Size)
	token = append(token, payload...)
	token = append(token, mac.Sum(nil)...)
	return base64.RawURLEncoding.EncodeToString(token), nil
}

func (c *inventoryCursorCodec) Decode(value string, principal Principal, projectID uuid.UUID, query string) (uuid.UUID, error) {
	binding := principalCursorBinding(principal)
	if c == nil || len(c.key) == 0 || value == "" || principal.OrganizationID == "" || binding == "" || projectID == uuid.Nil || normalizeInventoryQuery(query) != "" {
		return uuid.Nil, ErrInventoryCursorInvalid
	}
	token, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(token) <= sha256.Size {
		return uuid.Nil, ErrInventoryCursorInvalid
	}
	payload, signature := token[:len(token)-sha256.Size], token[len(token)-sha256.Size:]
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return uuid.Nil, ErrInventoryCursorInvalid
	}
	var cursor inventoryCursor
	if err := json.Unmarshal(payload, &cursor); err != nil || cursor.OrganizationID != principal.OrganizationID || cursor.Binding != binding || cursor.ProjectID != projectID.String() || cursor.Query != "" {
		return uuid.Nil, ErrInventoryCursorInvalid
	}
	after, err := uuid.Parse(cursor.AfterMCPID)
	if err != nil {
		return uuid.Nil, ErrInventoryCursorInvalid
	}
	return after, nil
}

func inventoryRegistrationIDs(rows []platformrepo.ListPlatformMCPInventoryRow) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.RegistrationID != uuid.Nil {
			ids = append(ids, row.RegistrationID)
		}
	}
	return ids
}

func inventoryDistributions(rows []platformrepo.ListPlatformMCPInventoryDistributionsRow) map[uuid.UUID][]MCPDistribution {
	byRegistration := make(map[uuid.UUID][]MCPDistribution)
	for _, row := range rows {
		byRegistration[row.RegistrationID] = append(byRegistration[row.RegistrationID], MCPDistribution{
			PluginID:         row.PluginID.String(),
			State:            row.State,
			PublicationState: row.PublicationState,
		})
	}
	return byRegistration
}

func mcpFromInventoryRow(row platformrepo.ListPlatformMCPInventoryRow, distributions map[uuid.UUID][]MCPDistribution) MCP {
	return mcpFromInventory(
		row.McpServerID, row.ProjectID, row.ProjectName, row.ProjectSlug, row.McpName.String, row.McpSlug.String, row.Visibility,
		inventoryModel(row.RemoteMcpServerID, row.TunneledMcpServerID, row.UnproxiedMcpServerID), row.RegistrationID, row.SourceKind, row.CatalogProvider, row.CatalogReference, row.RegistrationStatus,
		row.RegistrationRemoteMcpServerID, row.RegistrationUserSessionIssuerID, row.RegistrationMcpServerID, row.RegistrationMcpEndpointID,
		row.ReadinessState, timestampString(row.ReadinessCheckedAt.Time, row.ReadinessCheckedAt.Valid), timestampString(row.ReadinessExpiresAt.Time, row.ReadinessExpiresAt.Valid), distributions,
	)
}

func mcpFromInventoryItem(row platformrepo.GetPlatformMCPInventoryItemRow, distributions map[uuid.UUID][]MCPDistribution) MCP {
	return mcpFromInventory(
		row.McpServerID, row.ProjectID, row.ProjectName, row.ProjectSlug, row.McpName.String, row.McpSlug.String, row.Visibility,
		inventoryModel(row.RemoteMcpServerID, row.TunneledMcpServerID, row.UnproxiedMcpServerID), row.RegistrationID, row.SourceKind, row.CatalogProvider, row.CatalogReference, row.RegistrationStatus,
		row.RegistrationRemoteMcpServerID, row.RegistrationUserSessionIssuerID, row.RegistrationMcpServerID, row.RegistrationMcpEndpointID,
		row.ReadinessState, timestampString(row.ReadinessCheckedAt.Time, row.ReadinessCheckedAt.Valid), timestampString(row.ReadinessExpiresAt.Time, row.ReadinessExpiresAt.Valid), distributions,
	)
}

func mcpFromInventory(id, projectID uuid.UUID, projectName, projectSlug, name, slug, visibility, model string, registrationID uuid.UUID, sourceKind, provider, reference, registrationStatus string, registrationRemoteID, registrationIssuerID, registrationMCPID, registrationEndpointID uuid.NullUUID, readinessState, checkedAt, expiresAt string, distributions map[uuid.UUID][]MCPDistribution) MCP {
	mcp := MCP{
		ID:               id.String(),
		ProjectID:        projectID.String(),
		ProjectName:      projectName,
		ProjectSlug:      projectSlug,
		Name:             name,
		Slug:             slug,
		Version:          "",
		Visibility:       visibility,
		EffectiveEnabled: visibility != "disabled",
		Model:            "",
		Source:           MCPSource{Kind: "", Provider: "", Reference: ""},
		Registration:     nil,
		Readiness:        MCPReadiness{State: "", CheckedAt: "", ExpiresAt: ""},
		Distributions:    []MCPDistribution{},
		Operations:       []string{"read"},
		DashboardPath:    "",
	}

	switch {
	case registrationID != uuid.Nil:
		mcp.Model = "platform_managed"
		mcp.Source = MCPSource{Kind: inventorySourceKind(sourceKind), Provider: provider, Reference: reference}
		mcp.Registration = &MCPRegistration{
			ID:                 registrationID.String(),
			Status:             registrationStatus,
			ComponentsComplete: registrationRemoteID.UUID != uuid.Nil && registrationIssuerID.UUID != uuid.Nil && registrationMCPID.UUID != uuid.Nil && registrationEndpointID.UUID != uuid.Nil,
		}
		mcp.Readiness = MCPReadiness{State: "unknown", CheckedAt: "", ExpiresAt: ""}
		if readinessState != "" {
			mcp.Readiness = MCPReadiness{State: readinessState, CheckedAt: checkedAt, ExpiresAt: expiresAt}
		}
		if registeredDistributions := distributions[registrationID]; registeredDistributions != nil {
			mcp.Distributions = registeredDistributions
		}
		mcp.Operations = []string{"read", "dashboard_setup"}
		mcp.DashboardPath = "dashboard_mcp_settings"
	case model == "dashboard_managed":
		mcp.Model = model
		mcp.Source = MCPSource{Kind: "dashboard_source", Provider: "", Reference: ""}
		mcp.Readiness = MCPReadiness{State: "unsupported", CheckedAt: "", ExpiresAt: ""}
		mcp.DashboardPath = "dashboard_mcp_settings"
	default:
		mcp.Model = "legacy"
		mcp.Source = MCPSource{Kind: "legacy", Provider: "", Reference: ""}
		// Never infer or probe legacy readiness. The stored Platform readiness
		// table applies only to Platform registrations.
		mcp.Readiness = MCPReadiness{State: "unsupported", CheckedAt: "", ExpiresAt: ""}
		mcp.DashboardPath = "dashboard_mcp_settings"
	}
	return mcp
}

func inventoryModel(remote, tunneled, unproxied uuid.NullUUID) string {
	if remote.Valid || tunneled.Valid || unproxied.Valid {
		return "dashboard_managed"
	}
	return "legacy"
}

func inventorySourceKind(sourceKind string) string {
	switch sourceKind {
	case "catalog":
		return "reviewed_catalogue"
	case "remote_url":
		return "user_supplied_url"
	default:
		return sourceKind
	}
}

func timestampString(value time.Time, valid bool) string {
	if !valid {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
