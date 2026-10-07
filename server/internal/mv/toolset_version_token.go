package mv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"

	"github.com/google/uuid"
)

// ToolListVersionToken binds a version token to an exact target and the exact
// committed tool list. Two toolsets at the same version number never share a
// token, and a list edited and reverted between reads yields the token it
// started with — which is the honest answer, because the committed list is the
// same one.
//
// It is the one derivation behind both the Platform MCP exposure_version
// (scoped to an MCP server, so mcpServerID is set) and the toolset
// version_token the dashboard sends back on toolsets.update (scoped to the
// toolset alone, so mcpServerID is uuid.Nil and omitted from the preimage).
//
// The digest is deliberately unkeyed. Its entire preimage is the committed list
// itself, so anyone who can compute it already knows the state a read would
// have returned; there is nothing left for a signature to prove. Its job is
// detecting that the committed list moved between read and write, which an
// unkeyed digest does exactly as well. If the token ever starts carrying state
// the caller is not otherwise told — a principal, an expiry, a decision — it
// must become a keyed codec.
func ToolListVersionToken(projectID, mcpServerID, toolsetID uuid.UUID, version int64, urns []string) string {
	sorted := slices.Clone(urns)
	slices.Sort(sorted)
	var serverID string
	if mcpServerID != uuid.Nil {
		serverID = mcpServerID.String()
	}
	payload, err := json.Marshal(struct {
		ProjectID   string   `json:"project_id"`
		MCPServerID string   `json:"mcp_server_id,omitempty"`
		ToolsetID   string   `json:"toolset_id"`
		Version     int64    `json:"version"`
		ToolURNs    []string `json:"tool_urns"`
	}{projectID.String(), serverID, toolsetID.String(), version, sorted})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

// ToolsetVersionToken is the toolset-scoped ToolListVersionToken: the
// version_token a toolset read reports, and the value toolsets.update compares
// expected_version_token against under the toolset row lock.
func ToolsetVersionToken(projectID, toolsetID uuid.UUID, version int64, urns []string) string {
	return ToolListVersionToken(projectID, uuid.Nil, toolsetID, version, urns)
}
