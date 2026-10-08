package gram

import "github.com/urfave/cli/v2"

func internalCatalogFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "remote-mcp-catalog-ilb-cidr",
		Usage:   "Private IPv4 /32 for HTTPS MCP access to single-label hosts under catalog.dev.speakeasy.com; empty disables access",
		EnvVars: []string{"GRAM_REMOTE_MCP_CATALOG_ILB_CIDR"},
	}
}
