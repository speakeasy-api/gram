package hooks

import "context"

// NameMappingCache defines the interface for storing and retrieving name mappings
type NameMappingCache interface {
	// Get retrieves a name mapping from cache. Returns empty string if not found.
	Get(ctx context.Context, serverName string) (string, error)

	// Save stores a name mapping in cache
	Save(ctx context.Context, serverName, mappedName string) error
}
