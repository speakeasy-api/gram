package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/speakeasy-api/gram/cli/internal/workflow"
	"github.com/speakeasy-api/gram/server/gen/keys"
	"github.com/urfave/cli/v2"
)

// WhoamiOptions configures the Whoami operation
type WhoamiOptions struct {
	// ProfilePath overrides default (~/.gram/profile.json)
	ProfilePath string
	// ProfileName for multi-profile support (default: "default")
	ProfileName string
	// APIKey overrides profile's API key
	APIKey string
	// APIURL overrides profile's API URL
	APIURL string
}

// WhoamiResult contains the authenticated profile information
type WhoamiResult struct {
	Profile            *profile.Profile
	Organization       keys.ValidateKeyOrganization
	Scopes             []string
	CurrentProjectSlug string
	Projects           []*keys.ValidateKeyProject
}

// DoWhoami returns the current authenticated profile information
func DoWhoami(ctx context.Context, opts WhoamiOptions) (*WhoamiResult, error) {
	// Set defaults
	if opts.ProfilePath == "" {
		var err error
		opts.ProfilePath, err = profile.DefaultProfilePath()
		if err != nil {
			return nil, fmt.Errorf("failed to get profile path: %w", err)
		}
	}

	// Load profile - use current profile if no name specified
	var prof *profile.Profile
	var err error
	if opts.ProfileName != "" {
		prof, err = profile.LoadByName(opts.ProfilePath, opts.ProfileName)
	} else {
		prof, err = profile.Load(opts.ProfilePath)
	}
	if err != nil {
		return nil, fmt.Errorf("not authenticated: %w", err)
	}
	if prof == nil {
		return nil, fmt.Errorf("not authenticated: no profile found")
	}

	// Resolve API key
	apiKey := secret.Secret(opts.APIKey)
	if apiKey == "" {
		apiKey = secret.Secret(prof.Secret)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no API key found in options or profile")
	}

	// Resolve API URL
	apiURLStr := opts.APIURL
	if apiURLStr == "" {
		apiURLStr = prof.APIUrl
	}
	if apiURLStr == "" {
		apiURLStr = workflow.DefaultBaseURL
	}

	apiURL, err := url.Parse(apiURLStr)
	if err != nil {
		return nil, fmt.Errorf("invalid API URL: %w", err)
	}

	// Verify key with server
	client := api.NewKeysClient(&api.KeysClientOptions{
		Host:   apiURL.Host,
		Scheme: apiURL.Scheme,
	})

	result, err := client.Verify(ctx, apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to verify API key: %w", err)
	}

	// Resolve project slug
	projectSlug := prof.DefaultProjectSlug

	return &WhoamiResult{
		Profile:            prof,
		Organization:       *result.Organization,
		Scopes:             result.Scopes,
		CurrentProjectSlug: projectSlug,
		Projects:           result.Projects,
	}, nil
}

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

			result, err := DoWhoami(ctx, WhoamiOptions{
				APIKey: c.String("api-key"),
				APIURL: c.String("api-url"),
			})
			if err != nil {
				return fmt.Errorf("no profile configured, please set up a profile in $home/.gram/profile.json: %w", err)
			}

			if c.Bool("json") {
				out, err := json.MarshalIndent(ProfileInfo{
					Organization:       result.Organization,
					Scopes:             result.Scopes,
					CurrentProjectSlug: result.CurrentProjectSlug,
					Projects:           result.Projects,
				}, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to encode profile info: %w", err)
				}
				fmt.Println(string(out))
				return nil
			}

			printProfile(ProfileInfo{
				Organization:       result.Organization,
				Scopes:             result.Scopes,
				CurrentProjectSlug: result.CurrentProjectSlug,
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
