package core

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var syntheticTimestamp = time.Unix(0, 0).UTC().Format(time.RFC3339)

type ToolDescriptor struct {
	SourceSlug  string
	HandlerName string
	Name        string
	Description string
	InputSchema []byte
	Variables   map[string]any
	Annotations *types.ToolAnnotations
	Managed     bool
	OwnerKind   *string
	OwnerID     *string
}

func (d ToolDescriptor) ToolURN() urn.Tool {
	return urn.NewTool(urn.ToolKindPlatform, d.SourceSlug, d.HandlerName)
}

func (d ToolDescriptor) SyntheticID() string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(d.ToolURN().String())).String()
}

func (d ToolDescriptor) ToTool(projectID uuid.UUID) *types.Tool {
	schema := string(d.InputSchema)
	if schema == "" {
		schema = constants.DefaultEmptyToolSchema
	}

	return &types.Tool{
		PlatformToolDefinition: &types.PlatformToolDefinition{
			ID:            d.SyntheticID(),
			ToolUrn:       d.ToolURN().String(),
			ProjectID:     projectID.String(),
			Name:          d.Name,
			CanonicalName: d.Name,
			Description:   d.Description,
			SchemaVersion: nil,
			Schema:        schema,
			Confirm:       nil,
			ConfirmPrompt: nil,
			Summarizer:    nil,
			CreatedAt:     syntheticTimestamp,
			UpdatedAt:     syntheticTimestamp,
			Canonical:     nil,
			Variation:     nil,
			Annotations:   d.Annotations,
			SourceSlug:    d.SourceSlug,
			OwnerKind:     d.OwnerKind,
			OwnerID:       d.OwnerID,
		},
	}
}

func (d ToolDescriptor) ToToolEntry() *types.ToolEntry {
	return &types.ToolEntry{
		Type:    string(urn.ToolKindPlatform),
		ID:      d.SyntheticID(),
		Name:    d.Name,
		ToolUrn: d.ToolURN().String(),
	}
}

type PlatformToolExecutor interface {
	Descriptor() ToolDescriptor
	Call(ctx context.Context, payload io.Reader, wr io.Writer) error
}
