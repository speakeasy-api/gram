package platformmcp

import (
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

// openAIToolAllowlist is the whole catalogue a ChatGPT or Codex connection
// sees: the golden path from finding an MCP server to installing it. ChatGPT
// reviews every tool an app lists and shows them all to the user, and the
// ChatGPT desktop app, where the Speakeasy ChatGPT plugin runs, signs in as
// Codex and does not reliably surface a catalogue as large as the full
// administrator one. A short list that covers one complete outcome is easier
// to review and to use. list_projects is the one read outside that path: the
// default_project_not_found recovery tells the agent to call it.
//
// This is a usability filter, not a security boundary. Every caller is still
// entitled to every tool its grants allow; RBAC discovery filtering and each
// handler's own authorization apply on top of this list unchanged.
var openAIToolAllowlist = []string{
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

// openAIProductIDs are the aivendors registry products whose CIMD documents
// identify a caller limited to openAIToolAllowlist.
var openAIProductIDs = []string{"chatgpt", "codex"}

// openAIClientIDs are the CIMD document URLs of the openAIProductIDs products,
// resolved once because the registry is static.
var openAIClientIDs = sync.OnceValue(func() []string {
	var clientIDs []string
	for _, product := range aivendors.Products() {
		if slices.Contains(openAIProductIDs, product.ID) {
			clientIDs = append(clientIDs, aivendors.GatewayMatchersFor(product).ClientIDs...)
		}
	}
	return clientIDs
})

// isOpenAIClient reports whether a verified OAuth client_id belongs to ChatGPT
// or Codex. It resolves the client_id through the CIMD admission catalog the
// same way the MCP gateway does for AI tool blocks, then asks whether the entry
// that admitted it is one of those products' documents. A dynamically
// registered client has an opaque client_id that matches no entry, so it keeps
// the full catalogue.
func isOpenAIClient(clientID string) bool {
	preset, ok := admission.CatalogPreset(clientID)
	return ok && slices.Contains(openAIClientIDs(), preset.URL)
}

// clientToolAllowlist returns the tools a client is limited to, or nil when it
// may see the full catalogue.
func clientToolAllowlist(clientID string) []string {
	if isOpenAIClient(clientID) {
		return openAIToolAllowlist
	}
	return nil
}

// toolAllowedForClient reports whether a client's allowlist admits a tool.
func toolAllowedForClient(clientID, toolName string) bool {
	allowlist := clientToolAllowlist(clientID)
	return allowlist == nil || slices.Contains(allowlist, toolName)
}

var instructionToolNamePattern = regexp.MustCompile(`\b[a-z][a-z0-9]*(?:_[a-z0-9]+)+\b`)

// instructionsForClient drops every instruction paragraph that names a
// registered tool the client cannot see, so the instructions never send a
// limited client to a tool its catalogue does not have. Paragraphs that name no
// tool, or only admitted ones, are kept as written.
func (r *Registrar) instructionsForClient(clientID, instructions string) string {
	allowlist := clientToolAllowlist(clientID)
	if allowlist == nil || r == nil {
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
