package mcp

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

type resourcesTemplatesListResult struct {
	ResourceTemplates []*resourceTemplateListEntry `json:"resourceTemplates"`
}

type resourceTemplateListEntry struct {
	URITemplate string         `json:"uriTemplate"`
	Name        string         `json:"name"`
	Description *string        `json:"description,omitempty"`
	MimeType    *string        `json:"mimeType,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
}

// handleResourcesTemplatesList answers resources/templates/list with an empty
// list. Hosted servers advertise the resources capability and serve concrete
// resources via resources/list, but they do not expose URI templates.
func handleResourcesTemplatesList(ctx context.Context, logger *slog.Logger, req *rawRequest) (json.RawMessage, error) {
	result := &result[resourcesTemplatesListResult]{
		ID: req.ID,
		Result: resourcesTemplatesListResult{
			ResourceTemplates: make([]*resourceTemplateListEntry, 0),
		},
		serverIdentity: serverInfoHostedToolset,
	}

	bs, err := json.Marshal(result)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize resources/templates/list response").LogError(ctx, logger)
	}

	return bs, nil
}
