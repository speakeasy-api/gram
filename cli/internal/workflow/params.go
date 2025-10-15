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
	if c.IsSet("api-key") {
		return secret.Secret(c.String("api-key"))
	}

	if prof != nil {
		return secret.Secret(prof.Secret)
	}

	return secret.Secret("")
}

func ResolveProject(c *cli.Context, prof *profile.Profile) string {
	if c.IsSet("project") {
		return c.String("project")
	}

	if prof != nil {
		return prof.DefaultProjectSlug
	}

	return ""
}

func ResolveURL(c *cli.Context, prof *profile.Profile) (*url.URL, error) {
	var apiURLStr string
	switch {
	case c.IsSet("api-url"):
		apiURLStr = c.String("api-url")
	case prof != nil && prof.APIUrl != "":
		apiURLStr = prof.APIUrl
	default:
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
