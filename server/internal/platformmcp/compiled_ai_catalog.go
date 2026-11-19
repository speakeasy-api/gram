package platformmcp

import (
	"sync"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
)

// compiledAICatalog is the compiled registry until Platform MCP takes the injected catalog store.
var compiledAICatalog = sync.OnceValue(aivendors.CompiledSnapshot)
