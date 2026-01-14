package gram

import "github.com/urfave/cli/v2"

var pulseMCPFlags = []cli.Flag{
	&cli.StringFlag{
		Name:     "pulse-registry-tenant",
		Usage:    "The tenant ID used to communicate with the Pulse MCP registry",
		EnvVars:  []string{"PULSE_REGISTRY_TENANT"},
		Required: true,
	},
	&cli.StringFlag{
		Name:     "pulse-registry-api-key",
		Usage:    "The API key used to communicate with the Pulse MCP registry",
		EnvVars:  []string{"PULSE_REGISTRY_KEY"},
		Required: true,
	},
}
