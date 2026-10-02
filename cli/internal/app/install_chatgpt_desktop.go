package app

import (
	"fmt"
	"log/slog"

	"github.com/speakeasy-api/gram/cli/internal/app/logging"
	"github.com/urfave/cli/v2"
)

func newInstallChatGPTDesktopCommand() *cli.Command {
	return &cli.Command{
		Name:   "chatgpt-desktop",
		Usage:  "Print instructions for installing a Gram toolset in ChatGPT Desktop",
		Flags:  baseInstallFlags,
		Action: doInstallChatGPTDesktop,
	}
}

func doInstallChatGPTDesktop(c *cli.Context) error {
	ctx := c.Context
	logger := logging.PullLogger(ctx)
	info, err := resolveToolsetInfo(c)
	if err != nil {
		return fmt.Errorf("failed to resolve toolset info: %w", err)
	}

	logger.InfoContext(ctx, "prepared ChatGPT Desktop MCP install instructions",
		slog.String("name", info.Name),
		slog.String("url", info.URL))

	fmt.Printf("\n✓ ChatGPT Desktop install details for '%s'\n", info.Name)
	fmt.Printf("  MCP URL: %s\n", info.URL)
	fmt.Printf("\nChatGPT Desktop accepts remote HTTPS MCP servers only. There is no local config file or deep link to write.\n")
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("  1. Open ChatGPT Desktop → Settings and turn on Developer mode\n")
	fmt.Printf("     (Settings → Security and login, or Apps & Connectors → Advanced settings)\n")
	fmt.Printf("  2. Go to Settings → Apps & Connectors (or Plugins) and click Create\n")
	fmt.Printf("  3. Paste the MCP URL above as the connector URL\n")
	fmt.Printf("  4. Choose authentication: OAuth if the server requires sign-in, Token plus your Gram API key for private servers, or None for public servers\n")
	fmt.Printf("     ChatGPT Token auth sends Authorization: Bearer. Extra custom headers are not supported.\n")
	fmt.Printf("  5. Click Create, then enable the connector from the + menu in a new chat\n")

	return nil
}
