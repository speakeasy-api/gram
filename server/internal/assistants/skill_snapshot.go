package assistants

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

const assistantSkillSnapshotVersion = 1

type assistantSkillSetSnapshot struct {
	Version int                      `json:"version"`
	Skills  []assistantSkillSnapshot `json:"skills"`
}

type assistantSkillSnapshot struct {
	SkillID           uuid.UUID `json:"skill_id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	ResolvedVersionID uuid.UUID `json:"resolved_version_id"`
}

func newAssistantSkillSetSnapshot(rows []assistantSkillRow) assistantSkillSetSnapshot {
	snapshot := assistantSkillSetSnapshot{
		Version: assistantSkillSnapshotVersion,
		Skills:  make([]assistantSkillSnapshot, 0, len(rows)),
	}
	for _, row := range rows {
		snapshot.Skills = append(snapshot.Skills, assistantSkillSnapshot{
			SkillID:           row.SkillID,
			Name:              row.Name,
			Description:       row.Description,
			ResolvedVersionID: row.ResolvedVersionID,
		})
	}
	sort.Slice(snapshot.Skills, func(i, j int) bool {
		if snapshot.Skills[i].Name == snapshot.Skills[j].Name {
			return snapshot.Skills[i].SkillID.String() < snapshot.Skills[j].SkillID.String()
		}
		return snapshot.Skills[i].Name < snapshot.Skills[j].Name
	})
	return snapshot
}

func marshalAssistantSkillSetSnapshot(snapshot assistantSkillSetSnapshot) ([]byte, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal assistant skill snapshot: %w", err)
	}
	return raw, nil
}

func decodeAssistantSkillSetSnapshot(raw []byte) (assistantSkillSetSnapshot, error) {
	type skillDocument struct {
		SkillID           *uuid.UUID `json:"skill_id"`
		Name              *string    `json:"name"`
		Description       *string    `json:"description"`
		ResolvedVersionID *uuid.UUID `json:"resolved_version_id"`
	}
	var document struct {
		Version *int             `json:"version"`
		Skills  *[]skillDocument `json:"skills"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return assistantSkillSetSnapshot{}, fmt.Errorf("decode assistant skill snapshot: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return assistantSkillSetSnapshot{}, errors.New("decode assistant skill snapshot: trailing data")
		}
		return assistantSkillSetSnapshot{}, fmt.Errorf("decode assistant skill snapshot trailing data: %w", err)
	}
	if document.Version == nil {
		return assistantSkillSetSnapshot{}, errors.New("assistant skill snapshot version is required")
	}
	if *document.Version != assistantSkillSnapshotVersion {
		return assistantSkillSetSnapshot{}, fmt.Errorf("unsupported assistant skill snapshot version %d", *document.Version)
	}
	if document.Skills == nil {
		return assistantSkillSetSnapshot{}, errors.New("assistant skill snapshot skills must be an array")
	}
	snapshot := assistantSkillSetSnapshot{Version: *document.Version, Skills: make([]assistantSkillSnapshot, 0, len(*document.Skills))}
	seen := make(map[uuid.UUID]struct{}, len(*document.Skills))
	for _, skill := range *document.Skills {
		if skill.SkillID == nil || skill.ResolvedVersionID == nil || skill.Name == nil || skill.Description == nil || *skill.SkillID == uuid.Nil || *skill.ResolvedVersionID == uuid.Nil || strings.TrimSpace(*skill.Name) == "" {
			return assistantSkillSetSnapshot{}, errors.New("assistant skill snapshot contains an invalid skill")
		}
		if _, ok := seen[*skill.SkillID]; ok {
			return assistantSkillSetSnapshot{}, fmt.Errorf("assistant skill snapshot contains duplicate skill %s", *skill.SkillID)
		}
		seen[*skill.SkillID] = struct{}{}
		snapshot.Skills = append(snapshot.Skills, assistantSkillSnapshot{
			SkillID:           *skill.SkillID,
			Name:              *skill.Name,
			Description:       *skill.Description,
			ResolvedVersionID: *skill.ResolvedVersionID,
		})
	}
	return snapshot, nil
}

func renderAssistantSkillSetChange(previous, current assistantSkillSetSnapshot) string {
	previousByID := make(map[uuid.UUID]assistantSkillSnapshot, len(previous.Skills))
	currentByID := make(map[uuid.UUID]assistantSkillSnapshot, len(current.Skills))
	for _, skill := range previous.Skills {
		previousByID[skill.SkillID] = skill
	}
	for _, skill := range current.Skills {
		currentByID[skill.SkillID] = skill
	}

	added := make([]assistantSkillSnapshot, 0)
	updated := make([]assistantSkillSnapshot, 0)
	removed := make([]assistantSkillSnapshot, 0)
	for _, skill := range current.Skills {
		old, ok := previousByID[skill.SkillID]
		if !ok {
			added = append(added, skill)
		} else if old.ResolvedVersionID != skill.ResolvedVersionID || old.Name != skill.Name || old.Description != skill.Description {
			updated = append(updated, skill)
		}
	}
	for _, skill := range previous.Skills {
		if _, ok := currentByID[skill.SkillID]; !ok {
			removed = append(removed, skill)
		}
	}
	if len(added) == 0 && len(updated) == 0 && len(removed) == 0 {
		return ""
	}

	less := func(skills []assistantSkillSnapshot) {
		sort.Slice(skills, func(i, j int) bool {
			if skills[i].Name == skills[j].Name {
				return skills[i].SkillID.String() < skills[j].SkillID.String()
			}
			return skills[i].Name < skills[j].Name
		})
	}
	less(added)
	less(updated)
	less(removed)

	var b strings.Builder
	b.WriteString("<assistant-environment-change>\n")
	b.WriteString("EventType: assistant_skill_set_changed\n")
	b.WriteString("This section describes assistant environment state, not a user request.\n")
	if len(added) > 0 {
		b.WriteString("Added skills:\n")
		for _, skill := range added {
			name, description := safeSkillMetadata(skill)
			fmt.Fprintf(&b, "- Name: %s; description: %s. Call skills_load with name %s before relying on this skill.\n", strconv.Quote(name), strconv.Quote(description), strconv.Quote(name))
		}
	}
	if len(updated) > 0 {
		b.WriteString("Updated skills:\n")
		for _, skill := range updated {
			name, _ := safeSkillMetadata(skill)
			oldName, _ := safeSkillMetadata(previousByID[skill.SkillID])
			if oldName != name {
				fmt.Fprintf(&b, "- Name: %s (previously %s). This skill changed; call skills_load with name %s before relying on it.\n", strconv.Quote(name), strconv.Quote(oldName), strconv.Quote(name))
			} else {
				fmt.Fprintf(&b, "- Name: %s. This skill changed; call skills_load with name %s before relying on it.\n", strconv.Quote(name), strconv.Quote(name))
			}
		}
	}
	if len(removed) > 0 {
		b.WriteString("Removed skills:\n")
		for _, skill := range removed {
			name, _ := safeSkillMetadata(skill)
			fmt.Fprintf(&b, "- Name: %s. This skill is no longer available.\n", strconv.Quote(name))
		}
	}
	b.WriteString("</assistant-environment-change>")
	return b.String()
}

func safeSkillMetadata(skill assistantSkillSnapshot) (string, string) {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	name := conv.TruncateString(strings.Join(strings.Fields(skill.Name), " "), 200)
	description := conv.TruncateString(strings.Join(strings.Fields(skill.Description), " "), 200)
	return replacer.Replace(name), replacer.Replace(description)
}

func insertAssistantEnvironmentChange(prompt, notice string) (string, error) {
	if notice == "" {
		return prompt, nil
	}
	const closingTag = "</message-context>"
	index := strings.Index(prompt, closingTag)
	if index < 0 {
		return "", errors.New("assistant turn prompt has no message-context block")
	}
	return prompt[:index] + notice + "\n" + prompt[index:], nil
}
