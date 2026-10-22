package agentsurface

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// notProducts are hook sources that name a gateway or a model host rather
// than an agent product.
var notProducts = map[string]bool{"litellm": true, "bedrock": true, "aws-bedrock": true}

func TestEveryHookSourcePlacesAProduct(t *testing.T) {
	t.Parallel()

	for source := range bySource {
		if notProducts[source] {
			continue
		}
		require.NotEmpty(t, Place(source).Product, "hook source %q has no product", source)
	}
}
