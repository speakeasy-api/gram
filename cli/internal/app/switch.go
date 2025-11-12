package app

import (
	"fmt"

	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/urfave/cli/v2"
)

func newSwitchCommand() *cli.Command {
	return &cli.Command{
		Name:  "switch",
		Usage: "Switch the default project for the current profile",
		Description: `
Switch the default project for the current profile.

The project slug must be one of the projects available in your current profile.
Use 'gram status' to see your current project.`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "project",
				Usage:    "The project slug to switch to",
				Required: true,
			},
		},
		Action: func(c *cli.Context) error {
			projectSlug := c.String("project")

			profilePath, err := profile.DefaultProfilePath()
			if err != nil {
				return fmt.Errorf("failed to get profile path: %w", err)
			}

			if err := profile.UpdateProjectSlug(profilePath, projectSlug); err != nil {
				return fmt.Errorf("failed to switch project: %w", err)
			}

			fmt.Printf("Successfully switched to project: %s\n", projectSlug)
			return nil
		},
	}
}
