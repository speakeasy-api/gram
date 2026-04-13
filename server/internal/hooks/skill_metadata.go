package hooks

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

type skillTelemetryFields struct {
	Name             string
	Scope            string
	DiscoveryRoot    string
	SourceType       string
	ID               string
	VersionID        string
	ResolutionStatus string
}

func (s *Service) extractSkillTelemetryAttributes(ctx context.Context, additionalData map[string]any) map[attr.Key]any {
	if len(additionalData) == 0 {
		return nil
	}

	rawSkills, ok := additionalData["skills"]
	if !ok {
		return nil
	}

	skillsSlice, ok := rawSkills.([]any)
	if !ok {
		s.logger.WarnContext(ctx, "invalid additional_data.skills type")
		return nil
	}

	if len(skillsSlice) == 0 {
		return nil
	}

	for _, item := range skillsSlice {
		skillMap, ok := item.(map[string]any)
		if !ok {
			continue
		}

		fields := skillTelemetryFields{
			Name:             stringValue(skillMap["name"]),
			Scope:            stringValue(skillMap["scope"]),
			DiscoveryRoot:    stringValue(skillMap["discovery_root"]),
			SourceType:       stringValue(skillMap["source_type"]),
			ID:               stringValue(skillMap["skill_id"]),
			VersionID:        stringValue(skillMap["skill_version_id"]),
			ResolutionStatus: stringValue(skillMap["resolution_status"]),
		}

		attrs := map[attr.Key]any{}
		if fields.Name != "" {
			attrs[attr.SkillNameKey] = fields.Name
		}
		if fields.Scope != "" {
			attrs[attr.SkillScopeKey] = fields.Scope
		}
		if fields.DiscoveryRoot != "" {
			attrs[attr.SkillDiscoveryRootKey] = fields.DiscoveryRoot
		}
		if fields.SourceType != "" {
			attrs[attr.SkillSourceTypeKey] = fields.SourceType
		}
		if fields.ID != "" {
			attrs[attr.SkillIDKey] = fields.ID
		}
		if fields.VersionID != "" {
			attrs[attr.SkillVersionIDKey] = fields.VersionID
		}
		if fields.ResolutionStatus != "" {
			attrs[attr.SkillResolutionStatusKey] = fields.ResolutionStatus
		}

		if len(attrs) == 0 {
			continue
		}

		return attrs
	}

	return nil
}

func stringValue(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}

	return s
}
