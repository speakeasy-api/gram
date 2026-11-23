package agentsurface

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

// unplacedSources are hook sources aivendors deliberately names no product
// for: a gateway, a model host, or a product it does not list.
var unplacedSources = map[string]bool{
	"litellm":        true,
	"bedrock":        true,
	"aws-bedrock":    true,
	"copilot":        true,
	"github-copilot": true,
	"glean":          true,
}

func TestEveryHookSourceIdentifiesAnAIVendorsProduct(t *testing.T) {
	t.Parallel()

	for source := range bySource {
		if unplacedSources[source] {
			continue
		}
		_, ok := aivendors.Identify(source)
		require.True(t, ok, "hook source %q identifies no aivendors product", source)
	}
}
