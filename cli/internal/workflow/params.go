package workflow

import (
	"fmt"
	"net/url"

	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/urfave/cli/v2"
)

const DefaultBaseURL = "https://app.getgram.ai"

// ResolveParams extracts and validates parameters using the precedence
// chain:
//
// 1. CLI flags (if provided)
// 2. Environment variables (if set)
// 3. Profile values (if profile exists)
// 4. Defaults (for API URL only)
//
// Returns an error if required parameters (APIKey, ProjectSlug) cannot be
// resolved.
func ResolveParams(
	c *cli.Context,
	prof *profile.Profile,
) (Params, error) {
	apiKey := ResolveKey(c, prof)
	if apiKey == "" {
		return Params{}, fmt.Errorf("api-key required: not found in --api-key flag, $GRAM_API_KEY environment variable, or profile")
	}

	projectSlug := ResolveProject(c, prof)
	if projectSlug == "" {
		return Params{}, fmt.Errorf("project required: not found in --project flag, $GRAM_PROJECT environment variable, or profile")
	}

	apiURL, err := ResolveURL(c, prof)
	if err != nil {
		return Params{}, fmt.Errorf("failed to parse API URL: %w", err)
	}

	return Params{
		APIKey:      apiKey,
		APIURL:      apiURL,
		ProjectSlug: projectSlug,
	}, nil
}

func ResolveKey(c *cli.Context, prof *profile.Profile) secret.Secret {
	apiKey := c.String("api-key")
	if apiKey != "" {
		return secret.Secret(apiKey)
	}

	return secret.Secret(prof.Secret)
}

func ResolveProject(c *cli.Context, prof *profile.Profile) string {
	projectSlug := c.String("project")
	if projectSlug != "" {
		return projectSlug
	}

	return prof.DefaultProjectSlug
}

func ResolveURL(c *cli.Context, prof *profile.Profile) (*url.URL, error) {
	apiURLStr := c.String("api-url")
	if apiURLStr == "" && prof != nil {
		apiURLStr = prof.APIUrl
	}
	if apiURLStr == "" {
		apiURLStr = DefaultBaseURL
	}

	apiURL, err := url.Parse(apiURLStr)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to parse API URL '%s': %w",
			apiURLStr,
			err,
		)
	}

	return apiURL, nil
}
