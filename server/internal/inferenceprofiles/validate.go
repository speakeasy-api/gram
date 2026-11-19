package inferenceprofiles

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

var (
	// regionPattern accepts AWS (`us-west-2`, `us-gov-west-1`) and Google
	// Cloud (`us-east5`, `europe-west1`, `global`) region names.
	regionPattern = regexp.MustCompile(`^(global|[a-z]{2,12}(-[a-z0-9]{1,15}){1,3})$`)
	// awsProfilePattern is deliberately narrower than what AWS accepts: the
	// name is interpolated into the awsAuthRefresh shell command.
	awsProfilePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	// gcpProjectPattern follows Google Cloud's project ID rules.
	gcpProjectPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	// modelPattern admits Bedrock IDs and ARNs (`us.anthropic.claude-…-v1:0`,
	// `arn:aws:bedrock:…/…`) and Vertex IDs (`claude-…@20250929`).
	modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]{0,199}$`)
)

const maxBaseURLLength = 2048

// ValidateProfile checks a profile's provider and settings, returning a
// message suitable for the caller when they are invalid.
func ValidateProfile(provider string, settings Settings) error {
	var errs []error
	requireField := func(name, value string) {
		if value == "" {
			errs = append(errs, fmt.Errorf("%s is required for provider %s", name, provider))
		}
	}
	forbidField := func(name, value string) {
		if value != "" {
			errs = append(errs, fmt.Errorf("%s does not apply to provider %s", name, provider))
		}
	}

	switch provider {
	case ProviderBedrock:
		requireField("region", settings.Region)
		forbidField("gcp_project_id", settings.GCPProjectID)
	case ProviderVertex:
		requireField("region", settings.Region)
		requireField("gcp_project_id", settings.GCPProjectID)
		forbidField("aws_profile", settings.AWSProfile)
	case ProviderGateway:
		requireField("base_url", settings.BaseURL)
		forbidField("region", settings.Region)
		forbidField("aws_profile", settings.AWSProfile)
		forbidField("gcp_project_id", settings.GCPProjectID)
	default:
		return fmt.Errorf("provider must be one of %s, %s or %s", ProviderBedrock, ProviderVertex, ProviderGateway)
	}

	if settings.Region != "" && !regionPattern.MatchString(settings.Region) {
		errs = append(errs, errors.New("region must be a cloud region name such as us-west-2 or us-east5"))
	}
	if settings.AWSProfile != "" && !awsProfilePattern.MatchString(settings.AWSProfile) {
		errs = append(errs, errors.New("aws_profile may contain only letters, digits, '.', '_' and '-', up to 64 characters"))
	}
	if settings.GCPProjectID != "" && !gcpProjectPattern.MatchString(settings.GCPProjectID) {
		errs = append(errs, errors.New("gcp_project_id must be a Google Cloud project ID: 6 to 30 lowercase letters, digits and hyphens, starting with a letter"))
	}
	if settings.BaseURL != "" {
		if err := validateBaseURL(settings.BaseURL); err != nil {
			errs = append(errs, err)
		}
	}
	for name, model := range map[string]string{
		"models.default": settings.Models.Default,
		"models.opus":    settings.Models.Opus,
		"models.sonnet":  settings.Models.Sonnet,
		"models.haiku":   settings.Models.Haiku,
	} {
		if model != "" && !modelPattern.MatchString(model) {
			errs = append(errs, fmt.Errorf("%s is not a valid model ID", name))
		}
	}
	return joinSorted(errs)
}

func validateBaseURL(raw string) error {
	if len(raw) > maxBaseURLLength || strings.IndexFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return errors.New("base_url must be an https URL")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Opaque != "" {
		return errors.New("base_url must be an https URL")
	}
	if parsed.User != nil {
		return errors.New("base_url must not carry credentials")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.ForceQuery {
		return errors.New("base_url must not carry a query or fragment")
	}
	return nil
}

// ValidateHarness checks a rule's harness ("" means every harness) against
// the profile's provider.
func ValidateHarness(harness, provider string) error {
	switch harness {
	case "":
		return nil
	case HarnessClaudeCode, HarnessCodex:
		if !SupportsHarness(provider, harness) {
			return fmt.Errorf("%s cannot use a %s profile", harnessDisplayName(harness), provider)
		}
		return nil
	default:
		return fmt.Errorf("harness must be %s or %s", HarnessClaudeCode, HarnessCodex)
	}
}

func harnessDisplayName(harness string) string {
	switch harness {
	case HarnessClaudeCode:
		return "Claude Code"
	case HarnessCodex:
		return "Codex"
	default:
		return harness
	}
}

// joinSorted keeps multi-error messages stable regardless of map order.
func joinSorted(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		messages = append(messages, err.Error())
	}
	slices.Sort(messages)
	return errors.New(strings.Join(messages, "; "))
}
