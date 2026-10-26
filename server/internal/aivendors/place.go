package aivendors

import (
	"strings"
	"sync"
)

// Client is the kind of client a product runs in.
type Client string

const (
	// ClientUnknown is a name or signal that says nothing about the client.
	ClientUnknown Client = ""

	ClientCLI     Client = "cli"
	ClientIDE     Client = "ide"
	ClientDesktop Client = "desktop"
	ClientWeb     Client = "web"
	ClientMobile  Client = "mobile"
	ClientCloud   Client = "cloud"
	ClientSlack   Client = "slack"
)

// Surface is a name a producer reports for a product besides its MCP
// clientInfo names: a hook source or a telemetry dialect's surface.
type Surface struct {
	// Name is the reported name, matched after normalization.
	Name string

	// Client is the client the name implies, ClientUnknown when it names none.
	Client Client
}

type placement struct {
	productID string
	client    Client
}

// placements indexes every reported name by its normalized and its unspaced
// spelling. Surfaces win over clientInfo names, which imply no client.
var placements = sync.OnceValue(func() map[string]placement {
	index := map[string]placement{}
	add := func(name string, value placement) {
		normalized := normalize(name)
		for _, key := range []string{normalized, strings.ReplaceAll(normalized, "-", "")} {
			if _, taken := index[key]; !taken {
				index[key] = value
			}
		}
	}
	for _, product := range registry {
		for _, surface := range product.Surfaces {
			add(surface.Name, placement{productID: product.ID, client: surface.Client})
		}
	}
	for _, product := range registry {
		for _, name := range product.ClientInfoNames {
			add(name, placement{productID: product.ID, client: ClientUnknown})
		}
	}
	return index
})

// Place resolves a name a producer reported (a hook source, a dialect surface
// or an MCP clientInfo name) to the product it names and the client it
// implies. An unknown name places nowhere.
func Place(raw string) (productID string, client Client, ok bool) {
	normalized := normalize(raw)
	if normalized == "" {
		return "", ClientUnknown, false
	}
	index := placements()
	if found, ok := index[normalized]; ok {
		return found.productID, found.client, true
	}
	if found, ok := index[strings.ReplaceAll(normalized, "-", "")]; ok {
		return found.productID, found.client, true
	}
	return "", ClientUnknown, false
}

// normalize case-folds a name and unifies its separators to hyphens.
func normalize(raw string) string {
	lowered := strings.ToLower(strings.TrimSpace(raw))
	return strings.Join(strings.FieldsFunc(lowered, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '_' || r == '-'
	}), "-")
}

var ideTerminals = []string{"vscode", "cursor", "windsurf", "zed", "jetbrains", "intellij", "pycharm", "webstorm", "goland"}

// ClientForTerminal reads an editor's terminal as the IDE and any other as the CLI.
func ClientForTerminal(terminalType string) Client {
	normalized := strings.ToLower(strings.TrimSpace(terminalType))
	if normalized == "" {
		return ClientUnknown
	}
	if normalized == string(ClientIDE) {
		return ClientIDE
	}
	for _, ide := range ideTerminals {
		if strings.Contains(normalized, ide) {
			return ClientIDE
		}
	}
	return ClientCLI
}
