package plugins

import (
	"context"

	genplugins "github.com/speakeasy-api/gram/server/gen/plugins"
)

// PluginsService is the subset of the plugins management service that the
// managed assistant's plugin tools call. The concrete plugins service
// satisfies it; tools pass nil auth tokens because the assistant runtime
// supplies auth context out of band.
type PluginsService interface {
	ListPlugins(context.Context, *genplugins.ListPluginsPayload) (*genplugins.ListPluginsResult, error)
}
