package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/speakeasy-api/gram/cli/internal/api"
	"github.com/speakeasy-api/gram/cli/internal/flags"
	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/speakeasy-api/gram/server/gen/keys"
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
			flags.JSON(),
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

			result, err := client.Verify(ctx, workflowParams.APIKey)
			if err != nil {
				return fmt.Errorf("failed to verify API key: %w", err)
			}

			if c.Bool("json") {
				out, err := json.MarshalIndent(ProfileInfo{
					Organization:       *result.Organization,
					Scopes:             result.Scopes,
					CurrentProjectSlug: workflowParams.ProjectSlug,
					Projects:           result.Projects,
				}, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to encode profile info: %w", err)
				}
				fmt.Println(string(out))
				return nil
			}

			printProfile(ProfileInfo{
				Organization:       *result.Organization,
				Scopes:             result.Scopes,
				CurrentProjectSlug: workflowParams.ProjectSlug,
				Projects:           result.Projects,
			})

			return nil
		},
	}
}

type ProfileInfo struct {
	Organization       keys.ValidateKeyOrganization `json:"org"`
	Scopes             []string                     `json:"scopes"`
	CurrentProjectSlug string                       `json:"project"`
	Projects           []*keys.ValidateKeyProject   `json:"projects"`
}

func printProfile(profile ProfileInfo) {
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
			[]string{"Name:", profile.Organization.Name},
			[]string{"Slug:", profile.Organization.Slug},
		)

	fmt.Println(orgTable.Render())

	currentProjectIdx := slices.IndexFunc(profile.Projects, func(p *keys.ValidateKeyProject) bool {
		return p.Slug == profile.CurrentProjectSlug
	})

	currentProjectTable := table.New().
		Border(lipgloss.HiddenBorder()).
		StyleFunc(func(row, col int) lipgloss.Style {
			if col == 0 {
				return headerStyle
			}
			return lipgloss.NewStyle()
		}).
		Headers("TARGET PROJECT DETAILS").
		Rows(
			[]string{"Name:", profile.Projects[currentProjectIdx].Name},
			[]string{"Slug:", profile.Projects[currentProjectIdx].Slug},
		)

	fmt.Println(currentProjectTable.Render())

	fmt.Println(headerStyle.Margin(1).Render("API KEY SCOPES"))

	fmt.Println(lipgloss.NewStyle().MarginLeft(1).MarginBottom(1).Bold(true).Render(strings.Join(profile.Scopes, ", ")))

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
		Headers("ALL PROJECTS").
		Row("Name", "Slug")

	for _, proj := range profile.Projects {
		projectTable.Row(proj.Name, proj.Slug)
	}

	fmt.Println(projectTable.Render())
}
