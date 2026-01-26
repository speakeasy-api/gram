package telemetry

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// PosthogClient defines the interface for capturing events in PostHog.
type PosthogClient interface {
	CaptureEvent(ctx context.Context, eventName string, distinctID string, eventProperties map[string]interface{}) error
}

// ToolInfo represents the minimal tool information needed for logging
type ToolInfo struct {
	ID             string
	URN            string
	Name           string
	ProjectID      string
	DeploymentID   string
	FunctionID     *string
	OrganizationID string
}

func (t ToolInfo) AsAttributes() map[attr.Key]any {
	attrs := map[attr.Key]any{
		attr.ToolURNKey:        t.URN,
		attr.NameKey:           t.Name,
		attr.ProjectIDKey:      t.ProjectID,
		attr.DeploymentIDKey:   t.DeploymentID,
		attr.OrganizationIDKey: t.OrganizationID,
	}

	if t.FunctionID != nil {
		attrs[attr.FunctionIDKey] = *t.FunctionID
	}

	return attrs
}
