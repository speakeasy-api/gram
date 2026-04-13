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

		fields := skillTelemetryFields{}
		attrs := map[attr.Key]any{}
		setSkillFieldAndAttr(skillMap, "name", attr.SkillNameKey, &fields.Name, attrs)
		setSkillFieldAndAttr(skillMap, "scope", attr.SkillScopeKey, &fields.Scope, attrs)
		setSkillFieldAndAttr(skillMap, "discovery_root", attr.SkillDiscoveryRootKey, &fields.DiscoveryRoot, attrs)
		setSkillFieldAndAttr(skillMap, "source_type", attr.SkillSourceTypeKey, &fields.SourceType, attrs)
		setSkillFieldAndAttr(skillMap, "skill_id", attr.SkillIDKey, &fields.ID, attrs)
		setSkillFieldAndAttr(skillMap, "skill_version_id", attr.SkillVersionIDKey, &fields.VersionID, attrs)
		setSkillFieldAndAttr(skillMap, "resolution_status", attr.SkillResolutionStatusKey, &fields.ResolutionStatus, attrs)

		if len(attrs) == 0 {
			continue
		}

		return attrs
	}

	return nil
}

func setSkillFieldAndAttr(raw map[string]any, sourceKey string, attrKey attr.Key, field *string, attrs map[attr.Key]any) {
	value, ok := raw[sourceKey].(string)
	if !ok || value == "" {
		return
	}

	*field = value
	attrs[attrKey] = value
}
