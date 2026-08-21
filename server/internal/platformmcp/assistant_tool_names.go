package platformmcp

import (
	"slices"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/externalmcp"
)

// AssistantToolNames returns the names of every catalogue tool admitted to a
// project's managed assistant.
//
// The managed assistant's system prompt names its tools inline so the model
// can call them without discovery searches; this exists so a drift test can
// hold that prompt to the catalogue rather than letting it rot as tools are
// added or renamed.
//
// It composes the catalogue with no dependencies. Every capability registers
// an unavailable stub under the same name and audiences when its dependency is
// absent, so the name set is exactly the served one while the handlers are
// inert. Nothing here may be used to serve traffic.
func AssistantToolNames() []string {
	_, registrar := newServer(nil, nil, nil, "", nil, nil, nil, nil, nil, CatalogDescriptor{
		ProviderKey:      "",
		Registry:         externalmcp.Registry{ID: uuid.Nil, URL: ""},
		CanonicalRef:     "",
		AllowedRemoteURL: "",
		SetupIntent:      "",
	})

	descriptors := registrar.For(AudienceAssistant)
	names := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		names = append(names, descriptor.Name)
	}
	slices.Sort(names)
	return names
}
