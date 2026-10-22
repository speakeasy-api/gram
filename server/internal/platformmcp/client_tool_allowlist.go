package platformmcp

import (
	"regexp"
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

// chatGPTToolAllowlist is the whole catalogue a ChatGPT connection sees: the
// golden path from finding an MCP server to installing it. ChatGPT reviews
// every tool an app lists and shows them all to the user, so a short list that
// covers one complete outcome is easier to review and to use than the full
// administrator catalogue. list_projects is the one read outside that path:
// the default_project_not_found recovery tells the agent to call it.
//
// This is a usability filter, not a security boundary. Every caller is still
// entitled to every tool its grants allow; RBAC discovery filtering and each
// handler's own authorization apply on top of this list unchanged.
var chatGPTToolAllowlist = []string{
	"search_mcp_catalog",
	"inspect_mcp_candidate",
	"register_catalog_mcp",
	"register_remote_mcp",
	"attach_platform_mcp_identity_provider",
	"get_setup_handoff",
	"get_mcp_readiness",
	"get_my_install_instructions",
	"search_gram_docs",
	"list_projects",
}

// chatGPTProductID is the catalog product whose CIMD documents
// identify a ChatGPT caller.
const chatGPTProductID = "chatgpt"

// isChatGPTClient reports whether a verified OAuth client_id belongs to
// ChatGPT. It resolves the client_id through the CIMD admission catalog the
// same way the MCP gateway does for AI tool blocks, then asks whether the entry
// that admitted it is one of ChatGPT's documents. Resolving the entry rather
// than the vendor key keeps Codex, which OpenAI also publishes, out of this
// list. A dynamically registered client has an opaque client_id that matches
// no entry, so it keeps the full catalogue.
func isChatGPTClient(catalog *aivendors.Snapshot, clientID string) bool {
	preset, ok := catalog.CatalogPreset(clientID)
	return ok && preset.ProductID == chatGPTProductID
}

// clientToolAllowlist returns the tools a client is limited to, or nil when it
// may see the full catalogue.
func clientToolAllowlist(catalog *aivendors.Snapshot, clientID string) []string {
	if isChatGPTClient(catalog, clientID) {
		return chatGPTToolAllowlist
	}
	return nil
}

// toolAllowedForClient reports whether a client's allowlist admits a tool.
func toolAllowedForClient(catalog *aivendors.Snapshot, clientID, toolName string) bool {
	allowlist := clientToolAllowlist(catalog, clientID)
	return allowlist == nil || slices.Contains(allowlist, toolName)
}

var instructionToolNamePattern = regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b`)

// instructionsForClient drops every instruction paragraph that names a
// registered tool the client cannot see, so the instructions never send a
// limited client to a tool its catalogue does not have. Paragraphs that name no
// tool, or only admitted ones, are kept as written.
func (r *Registrar) instructionsForClient(clientID, instructions string) string {
	if r == nil {
		return instructions
	}
	allowlist := clientToolAllowlist(r.aiCatalog.Snapshot(), clientID)
	if allowlist == nil {
		return instructions
	}
	hidden := make(map[string]struct{}, len(r.descriptors))
	for _, descriptor := range r.descriptors {
		if !slices.Contains(allowlist, descriptor.Name) {
			hidden[descriptor.Name] = struct{}{}
		}
	}
	paragraphs := strings.Split(instructions, "\n\n")
	kept := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		if !slices.ContainsFunc(instructionToolNamePattern.FindAllString(paragraph, -1), func(name string) bool {
			_, ok := hidden[name]
			return ok
		}) {
			kept = append(kept, paragraph)
		}
	}
	return strings.Join(kept, "\n\n")
}
