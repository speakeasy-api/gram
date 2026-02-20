package telemetry

import (
	"context"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

// PosthogClient defines the interface for capturing events in PostHog.
type PosthogClient interface {
	CaptureEvent(ctx context.Context, eventName string, distinctID string, eventProperties map[string]any) error
}

// FeatureChecker is a function to determine whether a feature is enabled for an organization.
type FeatureChecker func(ctx context.Context, organisationID string) (bool, error)

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
		attr.OrganizationIDKey: t.OrganizationID,
	}

	if t.DeploymentID != "" {
		attrs[attr.DeploymentIDKey] = t.DeploymentID
	}

	if t.FunctionID != nil {
		attrs[attr.FunctionIDKey] = *t.FunctionID
	}

	return attrs
}
