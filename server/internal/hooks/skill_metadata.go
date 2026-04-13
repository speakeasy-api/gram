package hooks

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

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

	// Use the first skill item with at least one populated field and ignore the rest.
	for _, item := range skillsSlice {
		skillMap, ok := item.(map[string]any)
		if !ok {
			s.logger.WarnContext(ctx, "skipping non-map skill item")
			continue
		}

		attrs := map[attr.Key]any{}
		setSkillAttr(skillMap, "name", attr.SkillNameKey, attrs)
		setSkillAttr(skillMap, "scope", attr.SkillScopeKey, attrs)
		setSkillAttr(skillMap, "discovery_root", attr.SkillDiscoveryRootKey, attrs)
		setSkillAttr(skillMap, "source_type", attr.SkillSourceTypeKey, attrs)
		setSkillAttr(skillMap, "skill_id", attr.SkillIDKey, attrs)
		setSkillAttr(skillMap, "skill_version_id", attr.SkillVersionIDKey, attrs)
		setSkillAttr(skillMap, "resolution_status", attr.SkillResolutionStatusKey, attrs)

		if len(attrs) == 0 {
			continue
		}

		return attrs
	}

	return nil
}

func setSkillAttr(raw map[string]any, sourceKey string, attrKey attr.Key, attrs map[attr.Key]any) {
	value, ok := raw[sourceKey].(string)
	if !ok || value == "" {
		return
	}

	attrs[attrKey] = value
}
