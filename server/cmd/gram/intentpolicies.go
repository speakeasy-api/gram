package gram

import (
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/speakeasy-api/gram/server/internal/classifier"
	"github.com/speakeasy-api/gram/server/internal/classifier/jev"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/intentpolicy"
)

var intentPoliciesKeyFlag = &cli.StringFlag{
	Name:    "intent-policies-openrouter-api-key",
	Usage:   "Platform OpenRouter key for intent policy analysis; falls back to the sigint key, then locally to the OpenRouter dev key",
	EnvVars: []string{"GRAM_INTENT_POLICIES_OPENROUTER_API_KEY"},
}

// newIntentPoliciesClassifier returns the pinned Jev classifier, or a disabled
// one when no key resolves so analysis reports the missing key per call.
func newIntentPoliciesClassifier(c *cli.Context, guardianPolicy *guardian.Policy) (classifier.Classifier, error) {
	key := resolveIntentPoliciesKey(
		c.String(intentPoliciesKeyFlag.Name),
		os.Getenv("GRAM_SIGINT_OPENROUTER_API_KEY"),
		c.String("environment") == "local",
		c.String("openrouter-dev-key"),
	)
	if key == "" {
		return classifier.Noop{}, nil
	}
	client, err := jev.New(guardianPolicy, conv.NewSecret([]byte(key)), jev.WithModel(intentpolicy.JevModel))
	if err != nil {
		return nil, fmt.Errorf("create intent policies classifier: %w", err)
	}
	return client, nil
}

// resolveIntentPoliciesKey picks the first non-empty key. It is resolved here,
// not via EnvVars, which stops at a set-but-empty variable.
func resolveIntentPoliciesKey(intentKey, sigintKey string, local bool, devKey string) string {
	candidates := []string{intentKey, sigintKey}
	if local {
		candidates = append(candidates, devKey)
	}
	for _, k := range candidates {
		// mise.toml ships OPENROUTER_DEV_KEY="unset" as a placeholder.
		if k = strings.TrimSpace(k); k != "" && k != "unset" {
			return k
		}
	}
	return ""
}
