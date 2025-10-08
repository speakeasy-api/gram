package workflow

import (
	"fmt"
	"net/url"

	"github.com/speakeasy-api/gram/cli/internal/profile"
	"github.com/speakeasy-api/gram/cli/internal/secret"
	"github.com/urfave/cli/v2"
)

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
	apiKey := c.String("api-key")
	if apiKey == "" && prof != nil {
		apiKey = prof.Secret
	}
	if apiKey == "" {
		return Params{}, fmt.Errorf("api-key required: not found in --api-key flag, $GRAM_API_KEY environment variable, or profile")
	}

	projectSlug := c.String("project")
	if projectSlug == "" && prof != nil {
		projectSlug = prof.DefaultProjectSlug
	}
	if projectSlug == "" {
		return Params{}, fmt.Errorf("project required: not found in --project flag, $GRAM_PROJECT environment variable, or profile")
	}

	apiURLStr := c.String("api-url")
	if apiURLStr == "" && prof != nil {
		apiURLStr = prof.APIUrl
	}
	if apiURLStr == "" {
		apiURLStr = "https://app.getgram.ai"
	}

	apiURL, err := url.Parse(apiURLStr)
	if err != nil {
		return Params{}, fmt.Errorf(
			"failed to parse API URL '%s': %w",
			apiURLStr,
			err,
		)
	}

	return Params{
		APIKey:      secret.Secret(apiKey),
		APIURL:      apiURL,
		ProjectSlug: projectSlug,
	}, nil
}
