//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
)

// maxRegistryRecordInput leaves room for the record twice in a proposal: once
// as the stored argument and once in the approval preview.
const maxRegistryRecordInput = 24 << 10

type PrepareCreateRegistryEntryInput struct {
	RecordJSON string `json:"record_json" jsonschema:"Complete MCP registry server.json record as a JSON string, the same document the admin site registry editor accepts (up to 24 KiB)"`
	RetryKey   string `json:"retry_key" jsonschema:"Unique retry key for this exact change (up to 128 characters)"`
}

type registryCreateArguments struct {
	Record json.RawMessage `json:"record"`
}

type registryCreateExpected struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
}

type registryCreatePreview struct {
	Name        string                     `json:"name"`
	Version     string                     `json:"version"`
	Description string                     `json:"description"`
	Transports  []RegistryTransportSummary `json:"transports"`
	Record      json.RawMessage            `json:"record"`
}

type registryCreateReceipt struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UpdatedAt string `json:"updated_at"`
}

// registryEntryWriter executes one platform-global registry entry creation.
type registryEntryWriter struct {
	store    *proposalStore
	registry *mcpregistry.Service
	writes   WriteConfig
	baseURL  string
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
	projection, ok := projectRegistryRecord(compact.Bytes())
	if !ok {
		return nil, registryProjection{}, errors.New("record_json must include a server name, description and version, and only stdio, streamable-http or sse transports")
	}
	return compact.Bytes(), projection, nil
}

func registryCreateExpectedState(name string) (json.RawMessage, error) {
	encoded, err := json.Marshal(registryCreateExpected{Name: name, Exists: false})
	if err != nil {
		return nil, fmt.Errorf("encode expected registry state: %w", err)
	}
	return encoded, nil
}

func (s *registryEntryWriter) expectedArguments(ctx context.Context, tx pgx.Tx, proposal Proposal) (registryCreateArguments, error) {
	if proposal.Operation != OperationCreateRegistryEntry || proposal.SchemaVersion != 1 || !proposal.PlatformGlobal || proposal.Target.OrganizationID != "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceID != "" || proposal.Target.ResourceKind != "" {
		return registryCreateArguments{}, ErrProposalInvalidated
	}
	var args registryCreateArguments
	if err := json.Unmarshal(proposal.Arguments, &args); err != nil || len(args.Record) == 0 {
		return registryCreateArguments{}, ErrProposalInvalidated
	}
	_, projection, err := s.normalizeRegistryRecord(args.Record)
	if err != nil {
		return registryCreateArguments{}, ErrProposalInvalidated
	}
	taken, err := mcpregistry.NameTakenTx(ctx, tx, args.Record)
	if err != nil {
		return registryCreateArguments{}, fmt.Errorf("check registry entry name: %w", err)
	}
	if taken {
		return registryCreateArguments{}, ErrStaleState
	}
	expected, err := registryCreateExpectedState(projection.Name)
	if err != nil {
		return registryCreateArguments{}, err
	}
	digest, err := stateDigest(expected)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) {
		return registryCreateArguments{}, ErrStaleState
	}
	return args, nil
}

func (s *registryEntryWriter) revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	_, err := s.expectedArguments(ctx, tx, proposal)
	return err
}

func (s *registryEntryWriter) prepare(ctx context.Context, input PrepareCreateRegistryEntryInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, s.writes, OperationCreateRegistryEntry)
	if err != nil {
		return ProposalOutput{}, err
	}
	owner, err := ownerFromAuthority(authority)
	if err != nil {
		return ProposalOutput{}, err
	}
	if input.RetryKey == "" || len(input.RetryKey) > maxIdempotencyKeyLength {
		return ProposalOutput{}, errors.New("provide a retry key of at most 128 characters")
	}
	if input.RecordJSON == "" || len(input.RecordJSON) > maxRegistryRecordInput {
		return ProposalOutput{}, fmt.Errorf("provide record_json of at most %d bytes", maxRegistryRecordInput)
	}
	record, projection, err := s.normalizeRegistryRecord([]byte(input.RecordJSON))
	if err != nil {
		return ProposalOutput{}, err
	}
	tx, err := s.store.db.Begin(ctx)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("begin registry proposal preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	taken, err := mcpregistry.NameTakenTx(ctx, tx, record)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("check registry entry name: %w", err)
	}
	if taken {
		return ProposalOutput{}, errors.New("a registry entry with this server name already exists; edit it in the admin site instead")
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalOutput{}, fmt.Errorf("finish registry proposal preparation: %w", err)
	}
	arguments, err := json.Marshal(registryCreateArguments{Record: record})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode registry arguments: %w", err)
	}
	expected, err := registryCreateExpectedState(projection.Name)
	if err != nil {
		return ProposalOutput{}, err
	}
	preview, err := json.Marshal(registryCreatePreview{Name: projection.Name, Version: projection.Version, Description: projection.Description, Transports: projection.Transports, Record: record})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode registry preview: %w", err)
	}
	if len(arguments) > maxProposalArgumentBytes || len(preview) > maxProposalPreviewBytes {
		return ProposalOutput{}, errors.New("registry proposal exceeds the existing storage limit; shorten the record")
	}
	proposal, replay, err := s.store.Create(ctx, owner, NewProposal{Operation: OperationCreateRegistryEntry, SchemaVersion: 1, IdempotencyKey: input.RetryKey, Arguments: arguments, ExpectedState: expected, Preview: preview}, time.Now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, s.baseURL), nil
}

func (s *registryEntryWriter) view(_ context.Context, proposal Proposal) (proposalView, error) {
	if !proposal.PlatformGlobal || proposal.Target.OrganizationID != "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceID != "" || proposal.Target.ResourceKind != "" {
		return proposalView{}, ErrProposalInvalidated
	}
	var preview registryCreatePreview
	if err := json.Unmarshal(proposal.Preview, &preview); err != nil || preview.Name == "" || len(preview.Record) == 0 {
		return proposalView{}, ErrProposalInvalidated
	}
	var record bytes.Buffer
	if err := json.Indent(&record, preview.Record, "", "  "); err != nil {
		return proposalView{}, ErrProposalInvalidated
	}
	transports := "none"
	if len(preview.Transports) > 0 {
		transports = ""
		for i, transport := range preview.Transports {
			if i > 0 {
				transports += ", "
			}
			transports += transport.Type + " ×" + strconv.Itoa(transport.Count)
		}
	}
	return proposalView{
		Summary:        "Add " + preview.Name + " to the MCP registry, published to the customer catalog",
		PlatformGlobal: true,
		Changes: []proposalViewChange{
			{Setting: "Server name", Before: "(no entry)", After: preview.Name},
			{Setting: "Version", Before: "(no entry)", After: preview.Version},
			{Setting: "Description", Before: "(no entry)", After: preview.Description},
			{Setting: "Transports", Before: "(no entry)", After: transports},
			{Setting: "Record", Before: "(no entry)", After: record.String()},
		},
		SideEffects: "The entry is published as soon as it is created and appears in every organization's MCP catalog.",
	}, nil
}

func (s *registryEntryWriter) execution(_ writeAuthority) ProposalExecution {
	return ProposalExecution{Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
		args, err := s.expectedArguments(ctx, tx, proposal)
		if err != nil {
			return "", nil, err
		}
		created, err := s.registry.CreateTx(ctx, tx, args.Record)
		var invalid *mcpregistry.InvalidError
		switch {
		case errors.Is(err, mcpregistry.ErrConflict):
			return "", nil, ErrStaleState
		case errors.As(err, &invalid):
			return "", nil, ErrProposalInvalidated
		case err != nil:
			return "", nil, fmt.Errorf("create registry entry: %w", err)
		}
		projection, _ := projectRegistryRecord(created.Data)
		result, err := json.Marshal(registryCreateReceipt{ID: created.ID.String(), Name: projection.Name, UpdatedAt: mcpregistry.Token(created)})
		if err != nil {
			return "", nil, fmt.Errorf("encode registry receipt: %w", err)
		}
		return "succeeded", result, nil
	}}
}

func (s *registryEntryWriter) registerPrepare(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "prepare_create_registry_entry", Title: "Prepare Registry Entry", Description: "Prepare adding one new MCP server to the platform-global registry from a complete server.json record, as the admin site registry editor does. The record is validated against the registry schema and refused if its server name is already registered; editing or unpublishing existing entries is not supported. The entry is published on creation and appears in every organization's MCP catalog. The full record is stored for separate same-staff approval. To host an icon on Speakeasy, publish it with prepare_upload_platform_image first and use the receipt URL as server.icons[].src. Requires admin:write.", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true)}}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareCreateRegistryEntryInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := s.prepare(ctx, input)
		return nil, out, err
	})
}

var _ operationWriter = (*registryEntryWriter)(nil)
