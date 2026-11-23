package gram

import (
	"os"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	delivery "github.com/speakeasy-api/gram/server/internal/thirdparty/catalogpromotion"
	"github.com/urfave/cli/v2"
)

func catalogPromotionFlags() []cli.Flag {
	return []cli.Flag{
		&cli.BoolFlag{Name: "catalog-promotion-delivery-paused", EnvVars: []string{"GRAM_CATALOG_PROMOTION_DELIVERY_PAUSED"}, Usage: "Pause setup guide delivery while preserving pending jobs and candidate discovery"},
		&cli.StringFlag{Name: "catalog-guides-github-repository", EnvVars: []string{"GRAM_CATALOG_GUIDES_GITHUB_REPOSITORY"}, Value: delivery.DefaultGitHubRepository, Usage: "Repository running the MCP setup guide factory"},
	}
}

func catalogPromotionDelivery(c *cli.Context, policy *guardian.Policy) delivery.Client {
	return delivery.New(policy, delivery.Config{
		// Keep repository write credentials out of command-line arguments.
		GitHubToken:      os.Getenv("GRAM_CATALOG_GUIDES_GITHUB_TOKEN"),
		GitHubRepository: c.String("catalog-guides-github-repository"),
	})
}
