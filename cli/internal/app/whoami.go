package app

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/flags"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/urfave/cli/v2"
)

func newWhoAmICommand() *cli.Command {
	return &cli.Command{
		Name:  "whoami",
		Usage: "Display information about the profile currently in use",
		Description: `
Display information about the profile currently in use.

If no profile is configured, the command will indicate that no profile is set up.`,
		Flags: []cli.Flag{
			flags.APIEndpoint(),
			flags.APIKey(),
		},
		Action: func(c *cli.Context) error {
			ctx, cancel := signal.NotifyContext(
				c.Context,
				os.Interrupt,
				syscall.SIGTERM,
			)
			defer cancel()

			prof := profile.FromContext(ctx)

			workflowParams, err := workflow.ResolveParams(c, prof)
			if err != nil {
				return fmt.Errorf("no profile configured, please set up a profile in $home/.gram/profile.json")
			}

			client := api.NewKeysClient(&api.KeysClientOptions{
				Host:   workflowParams.APIURL.Host,
				Scheme: workflowParams.APIURL.Scheme,
			})

			result, err := client.VerifyKey(ctx, &api.VerifyKeyRequest{
				APIKey: workflowParams.APIKey,
			})
			if err != nil {
				return fmt.Errorf("failed to verify API key: %w", err)
			}

			headerStyle := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("6"))

			fmt.Println()
			fmt.Println(headerStyle.Render("Profile Information"))

			orgTable := table.New().
				Border(lipgloss.HiddenBorder()).
				StyleFunc(func(row, col int) lipgloss.Style {
					if col == 0 {
						return headerStyle
					}
					return lipgloss.NewStyle()
				}).
				Headers("ORGANIZATION DETAILS").
				Rows(
					[]string{"Name:", result.Organization.Name},
					[]string{"ID:", result.Organization.ID},
					[]string{"Slug:", result.Organization.Slug},
				)

			fmt.Println(orgTable.Render())

			fmt.Println(headerStyle.MarginLeft(1).MarginBottom(1).Render("API KEY SCOPES"))

			fmt.Println(lipgloss.NewStyle().MarginLeft(1).Bold(true).Render(strings.Join(result.Scopes, ", ")))

			projectTable := table.New().
				Border(lipgloss.HiddenBorder()).
				StyleFunc(func(row, col int) lipgloss.Style {
					if row <= 0 {
						return headerStyle
					}

					if col == 0 {
						return lipgloss.NewStyle().Bold(true)
					}

					return lipgloss.NewStyle()
				}).
				Headers("PROJECTS").
				Row("Name", "ID", "Slug")

			for _, proj := range result.Projects {
				projectTable.Row(proj.Name, proj.ID, proj.Slug)
			}

			fmt.Println(projectTable.Render())

			return nil
		},
	}

}
