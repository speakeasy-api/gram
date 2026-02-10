package app

import (
	"fmt"
	"net/url"

	"github.com/speakeasy-api/gram/cli/internal/api"
	//	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	//	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/urfave/cli/v2"
)

type Params struct {
	APIKey secret.Secret
}

func newSkillsCommand() *cli.Command {
	return &cli.Command{
		Name:        "skills",
		Usage:       "lol",
		Description: "foo",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "interactive",
				Usage: "ffs"},
			&cli.BoolFlag{
				Name:  "server-name",
				Usage: "lipsum",
			},
			&cli.StringSliceFlag{
				Name:  "tool-name",
				Usage: "stuff",
			},
			&cli.StringFlag{
				Name:  "api-key",
				Usage: "foo",
			},
			&cli.StringFlag{
				Name:  "project-slug",
				Usage: "foo",
			},
			&cli.StringFlag{
				Name: "url",
			},
		},
		Action: doSkills,
	}
}

func doSkills(c *cli.Context) error {
	// Print all flag values
	fmt.Printf("interactive: %v\n", c.Bool("interactive"))
	fmt.Printf("server-name: %v\n", c.Bool("server-name"))
	fmt.Printf("tool-name: %v\n", c.StringSlice("tool-name"))
	fmt.Printf("api-key: %v\n", c.String("api-key"))

	// Construct secret.Secret from string
	apiKey := secret.Secret(c.String("api-key"))
	projectSlug := c.String("project-slug")

	//prof := profile.FromContext(c.Context)
	parsedURL, err := url.Parse(c.String("url"))
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	ctx := c.Context
	tsc := api.NewToolsetsClient(&api.ToolsetsClientOptions{
		Scheme: parsedURL.Scheme,
		Host:   parsedURL.Host,
	})

	result, err := tsc.InferSkillsFromToolset(ctx, apiKey, projectSlug)
	if err != nil {
		return fmt.Errorf("could not infer skills from toolset: %w", err)
	}

	fmt.Printf("Tools: %v\n", result.Tools)
	fmt.Printf("Skills: %v\n", result.Skills)

	return nil
}
