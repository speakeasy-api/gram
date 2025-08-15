package usage

import (
	"context"
	"log/slog"

	polargo "github.com/polarsource/polar-go"
	polarComponents "github.com/polarsource/polar-go/models/components"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

type PolarClient struct {
	polar  *polargo.Polar
	logger *slog.Logger
}

func NewPolarClient(polar *polargo.Polar, logger *slog.Logger) *PolarClient {
	return &PolarClient{
		polar:  polar,
		logger: logger.With(attr.SlogComponent("polar-usage")),
	}
}

type ToolCallUsageEvent struct {
	OrganizationID   string
	RequestBytes     int64
	OutputBytes      int64
	ToolID           string
	ToolName         string
	ProjectID        string
	ProjectSlug      *string
	OrganizationSlug *string
	ToolsetSlug      *string
	ChatID           *string
}

func (p *PolarClient) TrackToolCallUsage(ctx context.Context, event ToolCallUsageEvent) {
	if p.polar == nil {
		return
	}

	totalBytes := event.RequestBytes + event.OutputBytes

	metadata := map[string]polarComponents.EventCreateExternalCustomerMetadata{
		"request_bytes": {
			Integer: &event.RequestBytes,
		},
		"output_bytes": {
			Integer: &event.OutputBytes,
		},
		"total_bytes": {
			Integer: &totalBytes,
		},
		"tool_id": {
			Str: &event.ToolID,
		},
		"tool_name": {
			Str: &event.ToolName,
		},
		"project_id": {
			Str: &event.ProjectID,
		},
	}

	if event.ProjectSlug != nil {
		metadata["project_slug"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.ProjectSlug,
		}
	}

	if event.OrganizationSlug != nil {
		metadata["organization_slug"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.OrganizationSlug,
		}
	}

	if event.ToolsetSlug != nil {
		metadata["toolset_slug"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.ToolsetSlug,
		}
	}

	if event.ChatID != nil {
		metadata["chat_id"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.ChatID,
		}
	}

	_, err := p.polar.Events.Ingest(context.Background(), polarComponents.EventsIngest{
		Events: []polarComponents.Events{
			{
				Type: polarComponents.EventsTypeEventCreateExternalCustomer,
				EventCreateExternalCustomer: &polarComponents.EventCreateExternalCustomer{
					ExternalCustomerID: event.OrganizationID,
					Name:               "tool-call",
					Metadata:           metadata,
				},
			},
		},
	})

	if err != nil {
		p.logger.ErrorContext(ctx, "failed to ingest usage event to Polar", attr.SlogError(err))
	}
}

type PromptCallUsageEvent struct {
	OrganizationID   string
	RequestBytes     int64
	OutputBytes      int64
	PromptID         *string
	PromptName       string
	ProjectID        string
	ProjectSlug      *string
	OrganizationSlug *string
	ToolsetSlug      *string
	ChatID           *string
}

func (p *PolarClient) TrackPromptCallUsage(ctx context.Context, event PromptCallUsageEvent) {
	if p.polar == nil {
		return
	}

	totalBytes := event.RequestBytes + event.OutputBytes

	metadata := map[string]polarComponents.EventCreateExternalCustomerMetadata{
		"request_bytes": {
			Integer: &event.RequestBytes,
		},
		"output_bytes": {
			Integer: &event.OutputBytes,
		},
		"total_bytes": {
			Integer: &totalBytes,
		},
		"prompt_name": {
			Str: &event.PromptName,
		},
		"project_id": {
			Str: &event.ProjectID,
		},
	}

	if event.PromptID != nil {
		metadata["prompt_id"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.PromptID,
		}
	}

	if event.ProjectSlug != nil {
		metadata["project_slug"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.ProjectSlug,
		}
	}

	if event.OrganizationSlug != nil {
		metadata["organization_slug"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.OrganizationSlug,
		}
	}

	if event.ToolsetSlug != nil {
		metadata["toolset_slug"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.ToolsetSlug,
		}
	}

	if event.ChatID != nil {
		metadata["chat_id"] = polarComponents.EventCreateExternalCustomerMetadata{
			Str: event.ChatID,
		}
	}

	_, err := p.polar.Events.Ingest(context.Background(), polarComponents.EventsIngest{
		Events: []polarComponents.Events{
			{
				Type: polarComponents.EventsTypeEventCreateExternalCustomer,
				EventCreateExternalCustomer: &polarComponents.EventCreateExternalCustomer{
					ExternalCustomerID: event.OrganizationID,
					Name:               "prompt-call",
					Metadata:           metadata,
				},
			},
		},
	})

	if err != nil {
		p.logger.ErrorContext(ctx, "failed to ingest usage event to Polar", attr.SlogError(err))
	}
}
