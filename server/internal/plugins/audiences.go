package plugins

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/database"
	pluginaudience "github.com/speakeasy-api/gram/server/internal/plugins/audience"
)

// Audience is the transport-neutral plugin audience projection.
type Audience = pluginaudience.Audience

// ResolveAudiences preserves the plugins package API while the dashboard and
// other internal callers share the leaf resolver implementation.
func ResolveAudiences(ctx context.Context, db database.DBTX, organizationID string) ([]Audience, error) {
	audiences, err := pluginaudience.Resolve(ctx, db, organizationID)
	if err != nil {
		return nil, fmt.Errorf("resolve plugin audiences: %w", err)
	}
	return audiences, nil
}
