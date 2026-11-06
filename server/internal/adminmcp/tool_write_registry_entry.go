//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
)

// maxRegistryRecordInput bounds the record a single tool call may submit.
const maxRegistryRecordInput = 24 << 10

// CreateRegistryEntryInput is one complete registry record.
type CreateRegistryEntryInput struct {
	RecordJSON string `json:"record_json" jsonschema:"Complete MCP registry server.json record as a JSON string, the same document the admin site registry editor accepts (up to 24 KiB)"`
}

type registryCreateReceipt struct {
	ID          string                     `json:"id"`
	Name        string                     `json:"name"`
	Version     string                     `json:"version"`
	Description string                     `json:"description"`
	Transports  []RegistryTransportSummary `json:"transports"`
	UpdatedAt   string                     `json:"updated_at"`
	Record      json.RawMessage            `json:"record"`
}

// registryEntryWriter applies one platform-global registry entry creation.
type registryEntryWriter struct {
	runner   *writeRunner
	registry *mcpregistry.Service
	writes   WriteConfig
}

// normalizeRegistryRecord validates a submitted record and returns its compact
// form with its safe projection.
func (s *registryEntryWriter) normalizeRegistryRecord(raw []byte) (json.RawMessage, registryProjection, error) {
	if issues := s.registry.Validate(raw); len(issues) > 0 {
		messages := make([]string, 0, min(len(issues), maxRegistryIssues))
		for _, issue := range issues[:min(len(issues), maxRegistryIssues)] {
			messages = append(messages, issue.Path+": "+issue.Message)
		}
		return nil, registryProjection{}, fmt.Errorf("record_json is not a valid registry record: %s", strings.Join(messages, "; "))
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return nil, registryProjection{}, errors.New("record_json must be one JSON object")
	}
	// Refuse state-independent metadata rejections here, so a record that can
	// never be created is not reported as stale.
	if err := mcpregistry.CheckCreateMetadata(compact.Bytes()); err != nil {
		var invalid *mcpregistry.InvalidError
		if errors.As(err, &invalid) && len(invalid.Issues) > 0 {
			return nil, registryProjection{}, fmt.Errorf("record_json is not a valid registry record: %s: %s", invalid.Issues[0].Path, invalid.Issues[0].Message)
		}
		return nil, registryProjection{}, errors.New("record_json has unreadable metadata")
	}
	// The input cap already bounds the record, so creation accepts every
	// schema-valid transport list rather than the read projection's bound.
	projection, ok := projectRegistryRecord(compact.Bytes(), math.MaxInt)
	if !ok {
		return nil, registryProjection{}, errors.New("record_json must include a server name, description and version, and only stdio, streamable-http or sse transports")
	}
	return compact.Bytes(), projection, nil
}

func (s *registryEntryWriter) apply(ctx context.Context, input CreateRegistryEntryInput) (registryCreateReceipt, error) {
	authority, err := requireWriteAuthority(ctx, s.writes, OperationCreateRegistryEntry)
	if err != nil {
		return registryCreateReceipt{}, err
	}
	if input.RecordJSON == "" || len(input.RecordJSON) > maxRegistryRecordInput {
		return registryCreateReceipt{}, fmt.Errorf("provide record_json of at most %d bytes", maxRegistryRecordInput)
	}
	record, projection, err := s.normalizeRegistryRecord([]byte(input.RecordJSON))
	if err != nil {
		return registryCreateReceipt{}, err
	}
	target := writeTarget{ResourceKind: "registry_entry", ResourceID: projection.Name}
	return runWrite(ctx, s.runner, authority, target, func(ctx context.Context, tx pgx.Tx) (registryCreateReceipt, error) {
		taken, err := mcpregistry.NameTakenTx(ctx, tx, record)
		if err != nil {
			return registryCreateReceipt{}, fmt.Errorf("check registry entry name: %w", err)
		}
		if taken {
			return registryCreateReceipt{}, errors.New("a registry entry with this server name already exists; edit it in the admin site instead")
		}
		created, err := s.registry.CreateTx(ctx, tx, record)
		if err != nil {
			return registryCreateReceipt{}, registryCreateError(err)
		}
		return registryCreateReceipt{ID: created.ID.String(), Name: projection.Name, Version: projection.Version, Description: projection.Description, Transports: projection.Transports, UpdatedAt: mcpregistry.Token(created), Record: record}, nil
	})
}

// registryCreateError maps a mutation failure. Input checks already refuse
// state-independent record problems, so a rejection here comes from registry
// state, such as a name taken by a concurrent create or an Okta mapping another
// entry now holds.
func registryCreateError(err error) error {
	var invalid *mcpregistry.InvalidError
	switch {
	case errors.Is(err, mcpregistry.ErrConflict), errors.As(err, &invalid):
		return ErrStaleState
	default:
		return fmt.Errorf("create registry entry: %w", err)
	}
}

func (s *registryEntryWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationCreateRegistryEntry), Title: "Create Registry Entry", Description: "Add one new MCP server to the platform-global registry from a complete server.json record, as the admin site registry editor does. The record is validated against the registry schema and refused if its server name is already registered; editing or unpublishing existing entries is not supported. The entry is published on creation and appears in every organization's MCP catalog. To host an icon on Speakeasy, upload it with create_image_upload_url first and use the returned SVG URL in envelope _meta[com.speakeasy.ai/icons]."}, s.apply)
}

var _ operationWriter = (*registryEntryWriter)(nil)
