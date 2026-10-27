package aivendors

import (
	"strings"
	"sync"
)

// Alias is another name a product is reported under, such as a hook source or
// a telemetry surface.
type Alias struct {
	// Name is the reported name, matched after normalization.
	Name string

	// Client is the client the name implies, ClientUnknown when it names none.
	Client Client
}

// Identity is the product a reported name identifies and the client it implies.
type Identity struct {
	// ProductID is the aivendors product id.
	ProductID string

	// Client is ClientUnknown when the name says nothing about the client.
	Client Client
}

// identities indexes every reported name by its normalized and its unspaced
// spelling. Aliases win over clientInfo names, which imply no client.
var identities = sync.OnceValue(func() map[string]Identity {
	index := map[string]Identity{}
	add := func(name string, identity Identity) {
		normalized := normalize(name)
		for _, key := range []string{normalized, strings.ReplaceAll(normalized, "-", "")} {
			if _, taken := index[key]; !taken {
				index[key] = identity
			}
		}
	}
	for _, product := range registry {
		for _, alias := range product.Aliases {
			add(alias.Name, Identity{ProductID: product.ID, Client: alias.Client})
		}
	}
	for _, product := range registry {
		for _, name := range product.ClientInfoNames {
			add(name, Identity{ProductID: product.ID, Client: ClientUnknown})
		}
	}
	return index
})

// Identify resolves a name a producer reported (an alias or an MCP clientInfo
// name) to its product and client. Every product and client the pipeline
// records comes from here.
func Identify(name string) (Identity, bool) {
	normalized := normalize(withoutMCPRemote(name))
	if normalized == "" {
		return Identity{ProductID: "", Client: ClientUnknown}, false
	}
	index := identities()
	if identity, ok := index[normalized]; ok {
		return identity, true
	}
	if identity, ok := index[strings.ReplaceAll(normalized, "-", "")]; ok {
		return identity, true
	}
	return Identity{ProductID: "", Client: ClientUnknown}, false
}

// mcpRemoteSuffix is how mcp-remote, the stdio bridge to a remote server,
// decorates the name of the client it relays for.
const mcpRemoteSuffix = " (via mcp-remote "

// withoutMCPRemote drops a trailing "(via mcp-remote x.y.z)" so the relayed
// client is identified by its own name.
func withoutMCPRemote(name string) string {
	trimmed := strings.TrimSpace(name)
	if index := strings.Index(trimmed, mcpRemoteSuffix); index > 0 && strings.HasSuffix(trimmed, ")") {
		return trimmed[:index]
	}
	return name
}

// normalize case-folds a name and unifies its separators to hyphens.
func normalize(raw string) string {
	lowered := strings.ToLower(strings.TrimSpace(raw))
	return strings.Join(strings.FieldsFunc(lowered, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '_' || r == '-'
	}), "-")
}
