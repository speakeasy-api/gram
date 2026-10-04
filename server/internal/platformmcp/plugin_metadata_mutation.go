package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	plugindelivery "github.com/speakeasy-api/gram/server/internal/plugins"
)

const (
	operationCreatePlugin = "create_plugin"
	operationRenamePlugin = "rename_plugin"

	pluginMetadataFeature = "plugin_metadata"

	// maxPluginMetadataNameBytes bounds a plugin name accepted here, matching
	// the display-name bound update_mcp_metadata applies to an MCP server.
	maxPluginMetadataNameBytes = 256

	// maxPluginMetadataDescriptionBytes bounds a plugin description accepted
	// here. A description is a sentence or two of package metadata; 1 KiB is
	// generous for that.
	maxPluginMetadataDescriptionBytes = 1024

	// maxPluginMetadataReceiptPayloadBytes bounds a stored create or rename
	// result. The input bounds above keep a valid result well inside it even
	// when every character is JSON-escaped to six bytes (two names and a
	// description come to about 9 KiB), so exceeding it means the result is
	// malformed rather than that the input was long.
	maxPluginMetadataReceiptPayloadBytes = 16 << 10 // 16 KiB

	// pluginMetadataCodeSlugTaken refuses a create whose slug another plugin in
	// the project already holds. It is distinct from
	// pluginMetadataCodeIdempotencyKeyReused because the two call for opposite
	// recoveries: a taken slug needs a new slug or the existing plugin, while a
	// reused key needs the original request repeated with its original key.
	pluginMetadataCodeSlugTaken = "slug_taken"

	// pluginMetadataCodeIdempotencyKeyReused refuses a request whose
	// idempotency key was already used for different input.
	pluginMetadataCodeIdempotencyKeyReused = "idempotency_key_reused"
)

var (
	ErrPluginMetadataMutationUnavailable = errors.New("platform mcp plugin metadata mutations unavailable")
	ErrPluginMetadataMutationInvalid     = errors.New("invalid platform mcp plugin metadata mutation")
	ErrPluginMetadataMutationConflict    = errors.New("platform mcp plugin metadata mutation conflict")
)

// PluginMetadataMutationError is safe to map into a tool refusal.
type PluginMetadataMutationError struct {
	Code    string
	Message string
	Cause   error
}

func (e *PluginMetadataMutationError) Error() string { return e.Message }
func (e *PluginMetadataMutationError) Unwrap() error { return e.Cause }

type CreatePluginInput struct {
	ProjectID      string `json:"project_id" jsonschema:"explicit project ID the plugin is created in"`
	Name           string `json:"name" jsonschema:"administrator-facing plugin name; at most 256 bytes"`
	Slug           string `json:"slug,omitempty" jsonschema:"optional slug of at most 60 lowercase letters, digits, and hyphens; derived from name when omitted. It becomes the plugin's permanent install name and does not change when the plugin is renamed"`
	Description    string `json:"description,omitempty" jsonschema:"optional plugin description; at most 1024 bytes"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact creation"`
	Confirmed      bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms the project, name, and slug of the plugin to create"`
}

type RenamePluginInput struct {
	ProjectID      string `json:"project_id" jsonschema:"explicit project ID that owns the plugin"`
	Plugin         string `json:"plugin" jsonschema:"exact plugin ID, slug, or name returned by list_plugins"`
	Name           string `json:"name" jsonschema:"new administrator-facing plugin name; at most 256 bytes"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact rename"`
	Confirmed      bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms the exact plugin and its new name"`
}

// PluginMetadataMutationPlugin is the committed plugin a create or rename
// produced, read inside the same transaction that wrote it.
type PluginMetadataMutationPlugin struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Slug        string                  `json:"slug"`
	Description string                  `json:"description,omitempty"`
	IsDefault   bool                    `json:"is_default"`
	ServerCount int64                   `json:"server_count"`
	SkillCount  int64                   `json:"skill_count"`
	Assignments PluginAssignmentSummary `json:"assignments"`
	Publication string                  `json:"publication"`
}

type PluginMetadataReceiptResult struct {
	ProjectID string                       `json:"project_id"`
	Plugin    PluginMetadataMutationPlugin `json:"plugin"`

	// PreviousName is the plugin's name before a rename. It is absent on a
	// create.
	PreviousName string `json:"previous_name,omitempty"`

	// DeliveredToEveryone reports that a new plugin was assigned to every
	// organization member because it lives in the organization's default
	// project. Elsewhere a new plugin reaches no one until it is assigned.
	DeliveredToEveryone bool `json:"delivered_to_everyone"`

	// PublicationRequest is what requesting a package refresh wrote alongside
	// the change.
	PublicationRequest string `json:"publication_request"`

	// ResultCategory is "created" or "renamed".
	ResultCategory string `json:"result_category"`
}

type PluginMetadataMutationOutput struct {
	PluginMetadataReceiptResult

	// PublishSignal reports whether the best-effort republish was requested
	// after commit. It is "not_requested" on a replay and when the durable
	// publication request already covers the change.
	PublishSignal string                  `json:"publish_signal"`
	Receipt       RiskMutationToolReceipt `json:"receipt"`
}

type normalizedPluginMetadataMutation struct {
	Operation   string `json:"operation"`
	ProjectID   string `json:"project_id"`
	Plugin      string `json:"plugin,omitempty"`
	Name        string `json:"name"`
	Slug        string `json:"slug,omitempty"`
	Description string `json:"description,omitempty"`
}

// WithMetadataMutations enables creating and renaming plugins. The writes go
// through the same core the plugins management API uses, so both surfaces
// refuse the same inputs the same way. There is deliberately no creation cap:
// the dashboard has none, and the operation budget already bounds a runaway
// create loop.
func (s *PluginsService) WithMetadataMutations(core *plugindelivery.PluginMetadataCore, publisher plugindelivery.PluginPublishSignaler, budget OperationBudget) *PluginsService {
	if s != nil {
		s.metadataCore = core
		s.metadataPublisher = publisher
		s.metadataBudget = budget
	}
	return s
}

func (s *PluginsService) metadataMutationValid() bool {
	return s.valid() && s.authorization != nil && s.metadataCore != nil && s.metadataBudget.valid()
}

func (s *PluginsService) CreatePlugin(ctx context.Context, principal Principal, input CreatePluginInput) (PluginMetadataMutationOutput, error) {
	if !s.metadataMutationValid() {
		return PluginMetadataMutationOutput{}, pluginMetadataMutationUnavailable(nil)
	}
	if !input.Confirmed {
		return PluginMetadataMutationOutput{}, pluginMetadataMutationInvalid("confirmation_required", "Ask the user to confirm the project, name, and slug of the plugin to create, then retry with confirmed: true.")
	}
	key := strings.TrimSpace(input.IdempotencyKey)
	if err := validatePluginMetadataRequest(principal, key, input.Name, input.Description); err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	// The core applies this same rule again inside the transaction. Checking
	// it here as well keeps a malformed slug from spending the allowance.
	if _, err := plugindelivery.ResolveCreatePluginSlug(input.Name, optionalString(input.Slug)); err != nil {
		return PluginMetadataMutationOutput{}, pluginMetadataCoreError(err)
	}
	project, err := s.authorizePluginMetadataProject(ctx, principal, input.ProjectID)
	if err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	normalized := normalizedPluginMetadataMutation{
		Operation: operationCreatePlugin, ProjectID: project.ID.String(), Plugin: "",
		Name: input.Name, Slug: input.Slug, Description: input.Description,
	}
	mutation := plugindelivery.CreatePluginMutation{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
		Name:           input.Name,
		Slug:           optionalString(input.Slug),
		Description:    optionalString(input.Description),
		Actor:          plugindelivery.PluginMetadataActor{UserID: principal.UserID, DisplayName: nil},
	}
	return s.executePluginMetadataMutation(ctx, principal, project, operationCreatePlugin, key, normalized, func(ctx context.Context, tx pgx.Tx) (PluginMetadataReceiptResult, error) {
		created, err := s.metadataCore.CreateInTransaction(ctx, tx, mutation)
		if err != nil {
			return PluginMetadataReceiptResult{}, pluginMetadataCoreError(err)
		}
		return s.pluginMetadataReceiptResult(ctx, tx, principal, project, created, "created")
	})
}

func (s *PluginsService) RenamePlugin(ctx context.Context, principal Principal, input RenamePluginInput) (PluginMetadataMutationOutput, error) {
	if !s.metadataMutationValid() {
		return PluginMetadataMutationOutput{}, pluginMetadataMutationUnavailable(nil)
	}
	if !input.Confirmed {
		return PluginMetadataMutationOutput{}, pluginMetadataMutationInvalid("confirmation_required", "Ask the user to confirm the exact plugin and its new name, then retry with confirmed: true.")
	}
	key := strings.TrimSpace(input.IdempotencyKey)
	if err := validatePluginMetadataRequest(principal, key, input.Name, ""); err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	target := strings.TrimSpace(input.Plugin)
	if target == "" {
		return PluginMetadataMutationOutput{}, pluginMetadataMutationInvalid("invalid_request", "Name the plugin to rename exactly as list_plugins returned it.")
	}
	if pluginID, err := uuid.Parse(target); err == nil && pluginID == uuid.Nil {
		return PluginMetadataMutationOutput{}, pluginMetadataMutationInvalid("invalid_request", "The plugin ID must not be all zeroes.")
	}
	project, err := s.authorizePluginMetadataProject(ctx, principal, input.ProjectID)
	if err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	normalized := normalizedPluginMetadataMutation{
		Operation: operationRenamePlugin, ProjectID: project.ID.String(), Plugin: target,
		Name: input.Name, Slug: "", Description: "",
	}
	return s.executePluginMetadataMutation(ctx, principal, project, operationRenamePlugin, key, normalized, func(ctx context.Context, tx pgx.Tx) (PluginMetadataReceiptResult, error) {
		// Resolved inside the receipt transaction, after the replay check, so a
		// retry of a rename that named the plugin by its old name replays
		// instead of failing to find it.
		ref, err := s.resolve(ctx, platformrepo.New(tx), principal, project.ID, target)
		if err != nil {
			return PluginMetadataReceiptResult{}, err
		}
		renamed, err := s.metadataCore.UpdateInTransaction(ctx, tx, plugindelivery.UpdatePluginMutation{
			OrganizationID:  principal.OrganizationID,
			ProjectID:       project.ID,
			PluginID:        ref.ID,
			Name:            input.Name,
			Slug:            nil,
			Description:     nil,
			KeepDescription: true,
			Actor:           plugindelivery.PluginMetadataActor{UserID: principal.UserID, DisplayName: nil},
		})
		if err != nil {
			return PluginMetadataReceiptResult{}, pluginMetadataCoreError(err)
		}
		result, err := s.pluginMetadataReceiptResult(ctx, tx, principal, project, renamed, "renamed")
		if err != nil {
			return PluginMetadataReceiptResult{}, err
		}
		if renamed.Before != nil {
			result.PreviousName = renamed.Before.Name
		}
		return result, nil
	})
}

// validatePluginMetadataRequest refuses out-of-bounds input before anything is
// read, charged, or written, so an over-long name is reported as an input
// limit rather than discovered as an oversized receipt after the write.
func validatePluginMetadataRequest(principal Principal, key, name, description string) error {
	if principal.OrganizationID == "" || principal.UserID == "" {
		return pluginMetadataMutationInvalid("invalid_request", "The plugin request is missing its caller identity.")
	}
	if key == "" || len(key) > 128 {
		return pluginMetadataMutationInvalid("invalid_request", "Provide a stable idempotency key of at most 128 characters so retrying this exact change is safe.")
	}
	if strings.TrimSpace(name) == "" {
		return pluginMetadataMutationInvalid("invalid_request", "Provide a plugin name.")
	}
	if len(name) > maxPluginMetadataNameBytes {
		return pluginMetadataMutationInvalid("invalid_request", fmt.Sprintf("A plugin name can be at most %d bytes. Ask the user for a shorter name.", maxPluginMetadataNameBytes))
	}
	if len(description) > maxPluginMetadataDescriptionBytes {
		return pluginMetadataMutationInvalid("invalid_request", fmt.Sprintf("A plugin description can be at most %d bytes. Ask the user for a shorter description.", maxPluginMetadataDescriptionBytes))
	}
	return nil
}

// authorizePluginMetadataProject checks plugin write against the project id the
// caller named before confirming that project exists, so a denial reads the
// same for a real project and an invented one. It is the check the dashboard's
// own plugin create and update make.
func (s *PluginsService) authorizePluginMetadataProject(ctx context.Context, principal Principal, projectID string) (ResolvedProject, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil {
		return ResolvedProject{}, ErrPluginProjectNotFound
	}
	if err := s.authorization.RequirePluginWrite(ctx, principal.OrganizationID, parsed.String()); err != nil {
		mapped := toolExposureAuthorizationError(err, authz.ScopePluginWrite)
		if _, ok := errors.AsType[*ExternalAuthorizationError](mapped); !ok {
			return ResolvedProject{}, pluginMetadataMutationUnavailable(err)
		}
		return ResolvedProject{}, mapped
	}
	project, err := s.resolveProject(ctx, platformrepo.New(s.db), principal, parsed.String())
	if err != nil {
		return ResolvedProject{}, err
	}
	return project, nil
}

// chargePluginMetadataMutation spends the create-and-rename allowance. It runs
// inside the receipt transaction's mutate step, which executeMutationReceipt
// reaches only after its replay lookup missed, so a retry of a committed
// change returns its stored result even when the allowance is spent, and a
// refused or replayed call never pays. A denial rolls back the pending receipt
// with everything else.
func (s *PluginsService) chargePluginMetadataMutation(ctx context.Context, principal Principal) error {
	if err := s.metadataBudget.AllowConnectionOrOrganization(ctx, principal); err != nil {
		if errors.Is(err, ErrOperationRateLimited) {
			return &PluginMetadataMutationError{Code: "rate_limited", Message: "Plugins were created or renamed too often just now. Try again shortly.", Cause: err}
		}
		return pluginMetadataMutationUnavailable(err)
	}
	return nil
}

func (s *PluginsService) executePluginMetadataMutation(ctx context.Context, principal Principal, project ResolvedProject, operation, key string, normalized normalizedPluginMetadataMutation, mutate func(context.Context, pgx.Tx) (PluginMetadataReceiptResult, error)) (PluginMetadataMutationOutput, error) {
	payload, err := json.Marshal(normalized)
	if err != nil {
		return PluginMetadataMutationOutput{}, pluginMetadataMutationInvalid("invalid_request", "The plugin request could not be normalized.")
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-plugin-metadata-v1\x00"+operation+"\x00"), payload...))
	receipt, err := executeMutationReceipt(ctx, mutationReceiptExecution[PluginMetadataReceiptResult]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operation,
		IdempotencyKey: key, InputHash: hex.EncodeToString(digest[:]), Label: "plugin metadata",
		Invalid: func(error) error {
			return pluginMetadataMutationInvalid("invalid_request", "The plugin request caller identity is invalid.")
		},
		// The executor reports a conflict only for the idempotency key itself:
		// reused with different input, or matching a change that never
		// completed. A taken slug is reported by the core instead, under its
		// own code.
		Conflict: func(string) error {
			return &PluginMetadataMutationError{
				Code:    pluginMetadataCodeIdempotencyKeyReused,
				Message: "This idempotency key was already used for a different plugin request. To retry the original request, repeat it exactly with its original key; for a new request, use a new key.",
				Cause:   ErrPluginMetadataMutationConflict,
			}
		},
		Unavailable:    pluginMetadataMutationUnavailable,
		ValidateReplay: validPluginMetadataReceiptPayload,
		EncodeResult:   encodePluginMetadataReceiptResult,
		Mutate: func(ctx context.Context, tx pgx.Tx) (PluginMetadataReceiptResult, error) {
			if err := s.chargePluginMetadataMutation(ctx, principal); err != nil {
				return PluginMetadataReceiptResult{}, err
			}
			return mutate(ctx, tx)
		},
	})
	if err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	var result PluginMetadataReceiptResult
	if err := json.Unmarshal(receipt.ResultPayload, &result); err != nil {
		return PluginMetadataMutationOutput{}, pluginMetadataMutationUnavailable(err)
	}
	output := PluginMetadataMutationOutput{PluginMetadataReceiptResult: result, PublishSignal: "not_requested", Receipt: riskMutationToolReceipt(receipt)}
	if !receipt.Replayed && result.PublicationRequest != string(plugindelivery.ProjectPublicationEnqueued) {
		switch {
		case s.metadataPublisher == nil:
			output.PublishSignal = "unavailable"
		case plugindelivery.SignalPluginPublishAfterRequest(ctx, s.metadataPublisher, plugindelivery.ProjectPublicationRequestOutcome(result.PublicationRequest), project.ID, principal.UserID) != nil:
			output.PublishSignal = "request_failed"
		default:
			output.PublishSignal = "best_effort_requested"
		}
	}
	return output, nil
}

// pluginMetadataReceiptResult reads the committed-to-be plugin back through the
// same inventory projection list_plugins returns, inside the write's
// transaction, so the result reports what was written rather than what was
// asked for.
func (s *PluginsService) pluginMetadataReceiptResult(ctx context.Context, tx pgx.Tx, principal Principal, project ResolvedProject, written plugindelivery.PluginMetadataResult, category string) (PluginMetadataReceiptResult, error) {
	row, err := platformrepo.New(tx).GetPlatformMCPPluginInventoryItem(ctx, platformrepo.GetPlatformMCPPluginInventoryItemParams{
		PluginID: written.Plugin.ID, ProjectID: project.ID, OrganizationID: principal.OrganizationID,
	})
	if err != nil {
		return PluginMetadataReceiptResult{}, fmt.Errorf("read written plugin: %w", err)
	}
	inventory := pluginFromInventoryRow(platformrepo.ListPlatformMCPPluginInventoryRow(row))
	if inventory.Assignments == nil {
		return PluginMetadataReceiptResult{}, pluginMetadataMutationUnavailable(errors.New("plugin assignment summary unavailable"))
	}
	return PluginMetadataReceiptResult{
		ProjectID: project.ID.String(),
		Plugin: PluginMetadataMutationPlugin{
			ID: inventory.ID, Name: inventory.Name, Slug: inventory.Slug, Description: inventory.Description,
			IsDefault: inventory.IsDefault, ServerCount: inventory.ServerCount, SkillCount: inventory.SkillCount,
			Assignments: *inventory.Assignments, Publication: inventory.Publication,
		},
		PreviousName:        "",
		DeliveredToEveryone: written.DeliveredToEveryone,
		PublicationRequest:  string(written.Publication),
		ResultCategory:      category,
	}, nil
}

func encodePluginMetadataReceiptResult(result PluginMetadataReceiptResult) ([]byte, error) {
	if !validPluginMetadataReceiptResult(result) {
		return nil, pluginMetadataMutationUnavailable(errors.New("unsafe plugin metadata receipt result"))
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("encode plugin metadata receipt result: %w", err)
	}
	if len(payload) > maxPluginMetadataReceiptPayloadBytes {
		return nil, pluginMetadataMutationUnavailable(errors.New("plugin metadata receipt result is too large"))
	}
	return payload, nil
}

func validPluginMetadataReceiptPayload(payload []byte) bool {
	var result PluginMetadataReceiptResult
	return len(payload) <= maxPluginMetadataReceiptPayloadBytes && json.Unmarshal(payload, &result) == nil && validPluginMetadataReceiptResult(result)
}

func validPluginMetadataReceiptResult(result PluginMetadataReceiptResult) bool {
	if uuid.Validate(result.ProjectID) != nil || uuid.Validate(result.Plugin.ID) != nil || result.Plugin.Slug == "" {
		return false
	}
	if result.ResultCategory != "created" && result.ResultCategory != "renamed" {
		return false
	}
	switch result.Plugin.Publication {
	case PluginPublicationPublished, PluginPublicationUnpublished, PluginPublicationNoRepository:
		return true
	default:
		return false
	}
}

// pluginMetadataCoreError turns the shared core's refusals into the readable
// refusals this surface returns. The wording matches what the dashboard says
// for the same input.
func pluginMetadataCoreError(err error) error {
	switch {
	case errors.Is(err, plugindelivery.ErrPluginSlugInvalid), errors.Is(err, plugindelivery.ErrPluginSlugTooLong), errors.Is(err, plugindelivery.ErrPluginNameWithoutSlug), errors.Is(err, plugindelivery.ErrPluginNameEmpty):
		return pluginMetadataMutationInvalid("invalid_request", err.Error()+".")
	case errors.Is(err, plugindelivery.ErrPluginSlugConflict):
		return &PluginMetadataMutationError{Code: pluginMetadataCodeSlugTaken, Message: "A plugin with this slug already exists in this project. List the project's plugins again, then either use that plugin or choose a different slug.", Cause: fmt.Errorf("%w: %w", ErrPluginMetadataMutationConflict, err)}
	case errors.Is(err, plugindelivery.ErrPluginMetadataTargetNotFound):
		return ErrPluginNotFound
	default:
		return err
	}
}

func pluginMetadataMutationInvalid(code, message string) error {
	return &PluginMetadataMutationError{Code: code, Message: message, Cause: ErrPluginMetadataMutationInvalid}
}

func pluginMetadataMutationUnavailable(cause error) error {
	if cause == nil {
		return &PluginMetadataMutationError{Code: unavailableCode, Message: "Creating and renaming plugins is not available on this server.", Cause: ErrPluginMetadataMutationUnavailable}
	}
	return &PluginMetadataMutationError{Code: unavailableCode, Message: "Creating and renaming plugins is temporarily unavailable.", Cause: fmt.Errorf("%w: %w", ErrPluginMetadataMutationUnavailable, cause)}
}
