package mv

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

// BuildTriggerInstanceView converts a repo trigger_instances row into the API
// response type.
func BuildTriggerInstanceView(instance triggerrepo.TriggerInstance, webhookURL *string) (*types.TriggerInstance, error) {
	config := map[string]any{}
	if len(instance.ConfigJson) > 0 {
		if err := json.Unmarshal(instance.ConfigJson, &config); err != nil {
			return nil, fmt.Errorf("decode trigger config: %w", err)
		}
		if config == nil {
			config = map[string]any{}
		}
	}

	return &types.TriggerInstance{
		ID:             instance.ID.String(),
		ProjectID:      instance.ProjectID.String(),
		DefinitionSlug: instance.DefinitionSlug,
		Name:           instance.Name,
		EnvironmentID:  conv.Ternary(instance.EnvironmentID.Valid, conv.PtrEmpty(instance.EnvironmentID.UUID.String()), nil),
		TargetKind:     instance.TargetKind,
		TargetRef:      instance.TargetRef,
		TargetDisplay:  instance.TargetDisplay,
		Config:         config,
		Status:         instance.Status,
		WebhookURL:     webhookURL,
		CreatedAt:      instance.CreatedAt.Time.Format(time.RFC3339),
		UpdatedAt:      instance.UpdatedAt.Time.Format(time.RFC3339),
	}, nil
}
