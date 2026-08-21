package gram

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/localfixture"
)

type platformMCPLocalFixtureConfig struct {
	Origin  *url.URL
	Fixture *localfixture.Config
}

func platformMCPLocalFixtureConfigFromCLI(environment, rawServerURL string) (*platformMCPLocalFixtureConfig, error) {
	if environment != "local" {
		return nil, nil
	}

	origin, err := url.Parse(rawServerURL)
	if err != nil {
		return nil, fmt.Errorf("parse server URL for local Platform MCP source: %w", err)
	}
	// The synthetic source includes OAuth and therefore requires HTTPS. Local
	// HTTP mode remains supported by using the normal browser-catalogue runtime
	// without the synthetic reviewed source instead of failing server startup.
	if origin.Scheme == "http" {
		return nil, nil
	}
	if err := localfixture.ValidateOrigin(origin); err != nil {
		return nil, fmt.Errorf("local Platform MCP source requires an HTTPS server origin without credentials, path, query, or fragment: %w", err)
	}

	origin.Path = strings.TrimSuffix(origin.Path, "/")
	fixture, err := localfixture.NewConfig(origin)
	if err != nil {
		return nil, fmt.Errorf("create local Platform MCP source descriptor: %w", err)
	}
	return &platformMCPLocalFixtureConfig{Origin: origin, Fixture: fixture}, nil
}
