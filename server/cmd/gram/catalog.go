package gram

import "github.com/urfave/cli/v2"

func internalCatalogFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "remote-mcp-catalog-ilb-cidr",
		Usage:   "Private IPv4 /32 for MCP catalog access over HTTPS on port 443; empty disables access",
		EnvVars: []string{"GRAM_REMOTE_MCP_CATALOG_ILB_CIDR"},
	}
}
