package inferenceprofiles

import (
	"strings"
	"time"

	gen "github.com/speakeasy-api/gram/server/gen/inference_profiles"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/inferenceprofiles/repo"
)

// Views live beside the service rather than in mv: they decode settings with
// this package's types, and mv importing this package would cycle through
// plugins.

func buildProfileView(row repo.InferenceProfile) (*gen.InferenceProfile, error) {
	profile, err := DecodeProfile(row)
	if err != nil {
		return nil, err
	}
	return &gen.InferenceProfile{
		ID:        row.ID.String(),
		Name:      row.Name,
		Provider:  row.Provider,
		Settings:  settingsView(profile.Settings),
		CreatedAt: row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}

func profileView(profile Profile) *gen.InferenceProfile {
	return &gen.InferenceProfile{
		ID:        profile.ID.String(),
		Name:      profile.Name,
		Provider:  profile.Provider,
		Settings:  settingsView(profile.Settings),
		CreatedAt: "",
		UpdatedAt: "",
	}
}

var noModels = Models{Default: "", Opus: "", Sonnet: "", Haiku: ""}

func settingsView(settings Settings) *gen.InferenceProfileSettings {
	var models *gen.InferenceProfileModels
	if settings.Models != noModels {
		models = &gen.InferenceProfileModels{
			Default: conv.PtrEmpty(settings.Models.Default),
			Opus:    conv.PtrEmpty(settings.Models.Opus),
			Sonnet:  conv.PtrEmpty(settings.Models.Sonnet),
			Haiku:   conv.PtrEmpty(settings.Models.Haiku),
		}
	}
	return &gen.InferenceProfileSettings{
		Region:       conv.PtrEmpty(settings.Region),
		AwsProfile:   conv.PtrEmpty(settings.AWSProfile),
		GcpProjectID: conv.PtrEmpty(settings.GCPProjectID),
		BaseURL:      conv.PtrEmpty(settings.BaseURL),
		Models:       models,
	}
}

func settingsFromView(view *gen.InferenceProfileSettings) Settings {
	if view == nil {
		var empty Settings
		return empty
	}
	settings := Settings{
		Region:       trimmed(view.Region),
		AWSProfile:   trimmed(view.AwsProfile),
		GCPProjectID: trimmed(view.GcpProjectID),
		BaseURL:      trimmed(view.BaseURL),
		Models:       noModels,
	}
	if view.Models != nil {
		settings.Models = Models{
			Default: trimmed(view.Models.Default),
			Opus:    trimmed(view.Models.Opus),
			Sonnet:  trimmed(view.Models.Sonnet),
			Haiku:   trimmed(view.Models.Haiku),
		}
	}
	return settings
}

func trimmed(value *string) string {
	return strings.TrimSpace(conv.PtrValOr(value, ""))
}

func buildRuleView(row repo.InferenceProfileRule) *gen.InferenceProfileRule {
	return &gen.InferenceProfileRule{
		ID:           row.ID.String(),
		ProfileID:    row.ProfileID.String(),
		PrincipalUrn: row.PrincipalUrn,
		Harness:      conv.FromPGText[string](row.Harness),
		Position:     int(row.Position),
		CreatedAt:    row.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:    row.UpdatedAt.Time.Format(time.RFC3339),
	}
}

func ruleView(rule Rule) *gen.InferenceProfileRule {
	return &gen.InferenceProfileRule{
		ID:           rule.ID.String(),
		ProfileID:    rule.ProfileID.String(),
		PrincipalUrn: rule.PrincipalURN,
		Harness:      conv.PtrEmpty(rule.Harness),
		Position:     rule.Position,
		CreatedAt:    rule.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    "",
	}
}

func buildPreviewView(resolutions []Resolution) *gen.PreviewInferenceProfilesResult {
	views := make([]*gen.InferenceProfileResolution, 0, len(resolutions))
	for _, resolution := range resolutions {
		view := &gen.InferenceProfileResolution{Harness: resolution.Harness, Profile: nil, Rule: nil}
		if resolution.Matched {
			view.Profile = profileView(resolution.Profile)
			view.Rule = ruleView(resolution.Rule)
		}
		views = append(views, view)
	}
	return &gen.PreviewInferenceProfilesResult{Resolutions: views, ToolConfig: ToolConfig(resolutions)}
}
