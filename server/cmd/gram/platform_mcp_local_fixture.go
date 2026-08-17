package gram

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/localfixture"
)

const platformMCPLocalFixtureFlag = "platform-mcp-local-fixture"

type platformMCPLocalFixtureConfig struct {
	Origin  *url.URL
	Fixture *localfixture.Config
}

func platformMCPLocalFixtureConfigFromCLI(environment string, enabled bool, rawServerURL string) (*platformMCPLocalFixtureConfig, error) {
	if !enabled {
		return nil, nil
	}
	if environment != "local" {
		return nil, fmt.Errorf("%s is only supported when environment is local", platformMCPLocalFixtureFlag)
	}

	origin, err := url.Parse(rawServerURL)
	if err != nil {
		return nil, fmt.Errorf("parse server URL for %s: %w", platformMCPLocalFixtureFlag, err)
	}
	if err := localfixture.ValidateOrigin(origin); err != nil {
		return nil, fmt.Errorf("%s requires an HTTPS server origin without credentials, path, query, or fragment: %w", platformMCPLocalFixtureFlag, err)
	}

	origin.Path = strings.TrimSuffix(origin.Path, "/")
	fixture, err := localfixture.NewConfig(origin)
	if err != nil {
		return nil, fmt.Errorf("create %s descriptor: %w", platformMCPLocalFixtureFlag, err)
	}
	return &platformMCPLocalFixtureConfig{Origin: origin, Fixture: fixture}, nil
}
