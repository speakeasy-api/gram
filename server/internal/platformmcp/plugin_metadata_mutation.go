package platformmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	plugindelivery "github.com/speakeasy-api/gram/server/internal/plugins"
)

const (
	operationCreatePlugin      = "create_plugin"
	operationRenamePlugin      = "rename_plugin"
	operationSetPluginAutoSync = "set_plugin_auto_sync"

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

	// pluginMetadataCodeUnsyncedChanges refuses turning auto sync off while the
	// plugin has changes that haven't been synced, as the dashboard does.
	pluginMetadataCodeUnsyncedChanges = "unsynced_changes"
)

// Plugin metadata result categories.
const (
	pluginMetadataCreated           = "created"
	pluginMetadataRenamed           = "renamed"
	pluginMetadataAutoSyncTurnedOn  = "auto_sync_turned_on"
	pluginMetadataAutoSyncTurnedOff = "auto_sync_turned_off"
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

	// Slug is the exact slug an unconfirmed create would give the plugin,
	// computed by the same resolver the create uses. It is set only on the
	// confirmation_required refusal, so an agent shows the user the real
	// install name instead of deriving one itself.
	Slug  string
	Cause error
}

func (e *PluginMetadataMutationError) Error() string { return e.Message }
func (e *PluginMetadataMutationError) Unwrap() error { return e.Cause }

type CreatePluginInput struct {
	ProjectID       string `json:"project_id" jsonschema:"explicit project ID the plugin is created in"`
	Name            string `json:"name" jsonschema:"administrator-facing plugin name; at most 256 bytes"`
	Slug            string `json:"slug,omitempty" jsonschema:"optional slug of at most 60 lowercase letters, digits, and hyphens; derived from name when omitted. It becomes the plugin's permanent install name and does not change when the plugin is renamed"`
	Description     string `json:"description,omitempty" jsonschema:"optional plugin description; at most 1024 bytes"`
	AutoSyncEnabled *bool  `json:"auto_sync_enabled,omitempty" jsonschema:"whether the plugin's changes publish to the marketplace on their own; defaults to true. When false, the plugin stays out of the marketplace until someone syncs it by hand"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact creation"`
	Confirmed       bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms the project, name, and slug of the plugin to create"`
}

type RenamePluginInput struct {
	ProjectID      string `json:"project_id" jsonschema:"explicit project ID that owns the plugin"`
	Plugin         string `json:"plugin" jsonschema:"exact plugin ID, slug, or name returned by list_plugins"`
	Name           string `json:"name" jsonschema:"new administrator-facing plugin name; at most 256 bytes"`
	IdempotencyKey string `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact rename"`
	Confirmed      bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms the exact plugin and its new name"`
}

type SetPluginAutoSyncInput struct {
	ProjectID       string `json:"project_id" jsonschema:"explicit project ID that owns the plugin"`
	Plugin          string `json:"plugin" jsonschema:"exact plugin ID, slug, or name returned by list_plugins"`
	AutoSyncEnabled bool   `json:"auto_sync_enabled" jsonschema:"true to publish the plugin's changes to the marketplace on their own; false to hold them until someone syncs by hand"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact change"`
	Confirmed       bool   `json:"confirmed" jsonschema:"set true only after the user explicitly confirms the exact plugin and whether its auto sync turns on or off"`
}

// PluginMetadataMutationPlugin is the committed plugin a create, rename, or
// auto sync change produced, read inside the same transaction that wrote it.
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

	// AutoSyncEnabled is absent from results stored before the setting was
	// reported.
	AutoSyncEnabled *bool `json:"auto_sync_enabled,omitempty"`
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

	// ResultCategory is "created", "renamed", "auto_sync_turned_on", or
	// "auto_sync_turned_off".
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

	// AutoSyncEnabled is recorded for an auto sync change, and for a create
	// only when it turns auto sync off, so a create that leaves it out hashes
	// as it did before the setting existed.
	AutoSyncEnabled *bool `json:"auto_sync_enabled,omitempty"`
}

// WithMetadataMutations enables creating and renaming plugins, and turning
// their auto sync on or off once WithAutoSyncGuard is composed too. The writes
// go through the same core the plugins management API uses, so both surfaces
// refuse the same inputs the same way. There is deliberately no creation cap:
// the dashboard has none, and the operation budget already bounds a runaway
// create loop. The post-commit publish signal goes through the same publisher
// WithRepublish composes.
func (s *PluginsService) WithMetadataMutations(logger *slog.Logger, core *plugindelivery.PluginMetadataCore, budget OperationBudget) *PluginsService {
	if s != nil {
		s.metadataLogger = logger
		s.metadataCore = core
		s.metadataBudget = budget
	}
	return s
}

func (s *PluginsService) metadataMutationValid() bool {
	return s.valid() && s.authorization != nil && s.metadataLogger != nil && s.metadataCore != nil && s.metadataBudget.valid()
}

// pluginAutoSyncGuard refuses turning a plugin's auto sync off while it has
// changes that haven't been synced. Implemented by the plugins management
// service, so the rule is the dashboard's own.
type pluginAutoSyncGuard interface {
	// RequireAutoSyncCanTurnOff checks in a transaction of its own, to refuse
	// before anything is charged or written.
	RequireAutoSyncCanTurnOff(ctx context.Context, organizationID string, projectID, pluginID uuid.UUID) error

	// RequireAutoSyncCanTurnOffInTransaction checks in the transaction that
	// writes the setting, so no change to the plugin commits unseen between
	// the check and the write.
	RequireAutoSyncCanTurnOffInTransaction(ctx context.Context, tx pgx.Tx, organizationID string, projectID, pluginID uuid.UUID) error
}

// WithAutoSyncGuard enables set_plugin_auto_sync alongside the other metadata
// writes.
func (s *PluginsService) WithAutoSyncGuard(guard pluginAutoSyncGuard) *PluginsService {
	if s != nil {
		s.autoSyncGuard = guard
	}
	return s
}

func (s *PluginsService) autoSyncMutationValid() bool {
	return s.metadataMutationValid() && s.autoSyncGuard != nil
}

// CreatePlugin creates one empty plugin. Every error it returns is a
// classified refusal; anything unexpected is logged with its cause and
// reported as temporarily unavailable.
func (s *PluginsService) CreatePlugin(ctx context.Context, principal Principal, input CreatePluginInput) (PluginMetadataMutationOutput, error) {
	output, err := s.createPlugin(ctx, principal, input)
	return output, s.pluginMetadataRefusal(ctx, operationCreatePlugin, err)
}

// RenamePlugin changes one plugin's display name. Every error it returns is a
// classified refusal; anything unexpected is logged with its cause and
// reported as temporarily unavailable.
func (s *PluginsService) RenamePlugin(ctx context.Context, principal Principal, input RenamePluginInput) (PluginMetadataMutationOutput, error) {
	output, err := s.renamePlugin(ctx, principal, input)
	return output, s.pluginMetadataRefusal(ctx, operationRenamePlugin, err)
}

// SetPluginAutoSync turns one plugin's auto sync on or off. Every error it
// returns is a classified refusal; anything unexpected is logged with its cause
// and reported as temporarily unavailable.
func (s *PluginsService) SetPluginAutoSync(ctx context.Context, principal Principal, input SetPluginAutoSyncInput) (PluginMetadataMutationOutput, error) {
	output, err := s.setPluginAutoSync(ctx, principal, input)
	return output, s.pluginMetadataRefusal(ctx, operationSetPluginAutoSync, err)
}

// pluginMetadataRefusal is the boundary between these writes and the MCP
// caller. Nothing behind it, such as the receipt executor, the shared core, or
// a repository read, is trusted to return caller-safe text, and the tool layer
// passes any error it cannot classify straight to the client. So an error that
// is not one of this surface's own refusals becomes the unavailable refusal,
// and every unavailable refusal logs the underlying cause rather than the
// sanitized message the caller sees.
func (s *PluginsService) pluginMetadataRefusal(ctx context.Context, operation string, err error) error {
	if err == nil {
		return nil
	}
	if refusal, ok := errors.AsType[*PluginMetadataMutationError](err); ok {
		if refusal.Code == unavailableCode {
			s.logPluginMetadataFailure(ctx, operation, refusal.Cause)
		}
		return err
	}
	if _, ok := errors.AsType[*ExternalAuthorizationError](err); ok {
		return err
	}
	if errors.Is(err, ErrPluginProjectNotFound) || errors.Is(err, ErrPluginNotFound) || errors.Is(err, ErrPluginAmbiguous) {
		return err
	}
	s.logPluginMetadataFailure(ctx, operation, err)
	return pluginMetadataMutationUnavailable(err)
}

func (s *PluginsService) logPluginMetadataFailure(ctx context.Context, operation string, cause error) {
	if s == nil || s.metadataLogger == nil {
		return
	}
	s.metadataLogger.ErrorContext(ctx, "platform mcp plugin metadata mutation failed", attr.SlogToolName(operation), attr.SlogError(cause))
}

func (s *PluginsService) createPlugin(ctx context.Context, principal Principal, input CreatePluginInput) (PluginMetadataMutationOutput, error) {
	if !s.metadataMutationValid() {
		return PluginMetadataMutationOutput{}, pluginMetadataMutationUnavailable(nil)
	}
	key := strings.TrimSpace(input.IdempotencyKey)
	if err := validatePluginMetadataRequest(principal, key, input.Name, input.Description); err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	// The core applies this same rule again inside the transaction. Resolving
	// it here as well keeps a malformed slug from spending the allowance, and
	// gives an unconfirmed call the exact slug to show the user.
	slug, err := plugindelivery.ResolveCreatePluginSlug(input.Name, optionalString(input.Slug))
	if err != nil {
		return PluginMetadataMutationOutput{}, pluginMetadataCoreError(err)
	}
	if !input.Confirmed {
		// Nothing is authorized, charged, or written for a preview: the slug
		// depends only on the input, and the confirmed call checks everything.
		return PluginMetadataMutationOutput{}, &PluginMetadataMutationError{
			Code:    "confirmation_required",
			Message: "Nothing was created. Show the user the project, the name, and this slug, which becomes the plugin's permanent install name, then call again with confirmed: true once they confirm.",
			Slug:    slug,
			Cause:   ErrPluginMetadataMutationInvalid,
		}
	}
	project, err := s.authorizePluginMetadataProject(ctx, principal, input.ProjectID)
	if err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	var autoSyncOff *bool
	if input.AutoSyncEnabled != nil && !*input.AutoSyncEnabled {
		autoSyncOff = new(false)
	}
	normalized := normalizedPluginMetadataMutation{
		Operation: operationCreatePlugin, ProjectID: project.ID.String(), Plugin: "",
		Name: input.Name, Slug: input.Slug, Description: input.Description, AutoSyncEnabled: autoSyncOff,
	}
	mutation := plugindelivery.CreatePluginMutation{
		OrganizationID:  principal.OrganizationID,
		ProjectID:       project.ID,
		Name:            input.Name,
		Slug:            optionalString(input.Slug),
		Description:     optionalString(input.Description),
		AutoSyncEnabled: input.AutoSyncEnabled,
		Actor:           plugindelivery.PluginMetadataActor{UserID: principal.UserID, DisplayName: nil},
	}
	return s.executePluginMetadataMutation(ctx, principal, project, operationCreatePlugin, key, normalized, nil, func(ctx context.Context, tx pgx.Tx) (PluginMetadataReceiptResult, error) {
		created, err := s.metadataCore.CreateInTransaction(ctx, tx, mutation)
		if err != nil {
			return PluginMetadataReceiptResult{}, pluginMetadataCoreError(err)
		}
		return s.pluginMetadataReceiptResult(ctx, tx, principal, project, created, pluginMetadataCreated)
	})
}

func (s *PluginsService) renamePlugin(ctx context.Context, principal Principal, input RenamePluginInput) (PluginMetadataMutationOutput, error) {
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
	target, err := validatePluginMetadataTarget(input.Plugin, "Name the plugin to rename exactly as list_plugins returned it.")
	if err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	project, err := s.authorizePluginMetadataProject(ctx, principal, input.ProjectID)
	if err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	normalized := normalizedPluginMetadataMutation{
		Operation: operationRenamePlugin, ProjectID: project.ID.String(), Plugin: target,
		Name: input.Name, Slug: "", Description: "", AutoSyncEnabled: nil,
	}
	return s.executePluginMetadataMutation(ctx, principal, project, operationRenamePlugin, key, normalized, nil, func(ctx context.Context, tx pgx.Tx) (PluginMetadataReceiptResult, error) {
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
			Name:            &input.Name,
			Slug:            nil,
			Description:     nil,
			KeepDescription: true,
			AutoSyncEnabled: nil,
			Actor:           plugindelivery.PluginMetadataActor{UserID: principal.UserID, DisplayName: nil},
		})
		if err != nil {
			return PluginMetadataReceiptResult{}, pluginMetadataCoreError(err)
		}
		result, err := s.pluginMetadataReceiptResult(ctx, tx, principal, project, renamed, pluginMetadataRenamed)
		if err != nil {
			return PluginMetadataReceiptResult{}, err
		}
		if renamed.Before != nil {
			result.PreviousName = renamed.Before.Name
		}
		return result, nil
	})
}

// setPluginAutoSync applies the dashboard's auto sync switch: turning auto
// sync off is refused while the plugin has changes that haven't been synced,
// and turning it on lets the publish this requests send what was held.
func (s *PluginsService) setPluginAutoSync(ctx context.Context, principal Principal, input SetPluginAutoSyncInput) (PluginMetadataMutationOutput, error) {
	if !s.autoSyncMutationValid() {
		return PluginMetadataMutationOutput{}, &PluginMetadataMutationError{Code: unavailableCode, Message: pluginAutoSyncUnavailableMessage, Slug: "", Cause: ErrPluginMetadataMutationUnavailable}
	}
	if !input.Confirmed {
		return PluginMetadataMutationOutput{}, pluginMetadataMutationInvalid("confirmation_required", "Ask the user to confirm the exact plugin and whether its auto sync turns on or off, then retry with confirmed: true.")
	}
	key := strings.TrimSpace(input.IdempotencyKey)
	if err := validatePluginMetadataCaller(principal, key); err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	target, err := validatePluginMetadataTarget(input.Plugin, "Name the plugin exactly as list_plugins returned it.")
	if err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	project, err := s.authorizePluginMetadataProject(ctx, principal, input.ProjectID)
	if err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	normalized := normalizedPluginMetadataMutation{
		Operation: operationSetPluginAutoSync, ProjectID: project.ID.String(), Plugin: target,
		Name: "", Slug: "", Description: "", AutoSyncEnabled: &input.AutoSyncEnabled,
	}
	category := pluginMetadataAutoSyncTurnedOff
	if input.AutoSyncEnabled {
		category = pluginMetadataAutoSyncTurnedOn
	}
	// The target is resolved and checked after the replay lookup, so a retry
	// of a change that committed replays it rather than being refused over
	// changes made since, and before the receipt transaction, so a refusal is
	// never charged. The write checks again in its own transaction, which is
	// what keeps a change to the plugin from committing unseen in between.
	var ref PluginRef
	precheck := func(ctx context.Context) error {
		resolved, err := s.resolve(ctx, platformrepo.New(s.db), principal, project.ID, target)
		if err != nil {
			return err
		}
		ref = resolved
		if input.AutoSyncEnabled {
			return nil
		}
		if err := s.autoSyncGuard.RequireAutoSyncCanTurnOff(ctx, principal.OrganizationID, project.ID, ref.ID); err != nil {
			return pluginMetadataCoreError(fmt.Errorf("check plugin has no unsynced changes: %w", err))
		}
		return nil
	}
	return s.executePluginMetadataMutation(ctx, principal, project, operationSetPluginAutoSync, key, normalized, precheck, func(ctx context.Context, tx pgx.Tx) (PluginMetadataReceiptResult, error) {
		if !input.AutoSyncEnabled {
			if err := s.autoSyncGuard.RequireAutoSyncCanTurnOffInTransaction(ctx, tx, principal.OrganizationID, project.ID, ref.ID); err != nil {
				return PluginMetadataReceiptResult{}, pluginMetadataCoreError(fmt.Errorf("check plugin has no unsynced changes: %w", err))
			}
		}
		updated, err := s.metadataCore.UpdateInTransaction(ctx, tx, plugindelivery.UpdatePluginMutation{
			OrganizationID:  principal.OrganizationID,
			ProjectID:       project.ID,
			PluginID:        ref.ID,
			Name:            nil,
			Slug:            nil,
			Description:     nil,
			KeepDescription: true,
			AutoSyncEnabled: &input.AutoSyncEnabled,
			Actor:           plugindelivery.PluginMetadataActor{UserID: principal.UserID, DisplayName: nil},
		})
		if err != nil {
			return PluginMetadataReceiptResult{}, pluginMetadataCoreError(err)
		}
		return s.pluginMetadataReceiptResult(ctx, tx, principal, project, updated, category)
	})
}

// validatePluginMetadataRequest refuses out-of-bounds input before anything is
// read, charged, or written, so an over-long name is reported as an input
// limit rather than discovered as an oversized receipt after the write.
func validatePluginMetadataRequest(principal Principal, key, name, description string) error {
	if err := validatePluginMetadataCaller(principal, key); err != nil {
		return err
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

// validatePluginMetadataTarget returns the exact plugin an update names, or
// refuses it with missing as the message when it names none.
func validatePluginMetadataTarget(plugin, missing string) (string, error) {
	target := strings.TrimSpace(plugin)
	if target == "" {
		return "", pluginMetadataMutationInvalid("invalid_request", missing)
	}
	if pluginID, err := uuid.Parse(target); err == nil && pluginID == uuid.Nil {
		return "", pluginMetadataMutationInvalid("invalid_request", "The plugin ID must not be all zeroes.")
	}
	return target, nil
}

func validatePluginMetadataCaller(principal Principal, key string) error {
	if principal.OrganizationID == "" || principal.UserID == "" {
		return pluginMetadataMutationInvalid("invalid_request", "The plugin request is missing its caller identity.")
	}
	if key == "" || len(key) > 128 {
		return pluginMetadataMutationInvalid("invalid_request", "Provide a stable idempotency key of at most 128 characters so retrying this exact change is safe.")
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

// chargePluginMetadataMutation spends the plugin metadata allowance. It runs
// after validation, authorization, and the replay lookup, and outside any
// transaction, so a refused or replayed call never pays and a slow limiter
// never holds a database connection.
func (s *PluginsService) chargePluginMetadataMutation(ctx context.Context, principal Principal) error {
	if err := s.metadataBudget.AllowConnectionOrOrganization(ctx, principal); err != nil {
		if errors.Is(err, ErrOperationRateLimited) {
			return &PluginMetadataMutationError{Code: "rate_limited", Message: "Plugins were created or changed too often just now. Try again shortly.", Slug: "", Cause: err}
		}
		return pluginMetadataMutationUnavailable(err)
	}
	return nil
}

// executePluginMetadataMutation runs mutate in the receipt transaction.
// precheck, when set, runs only when no stored receipt answers the request, and
// before the allowance is charged or the transaction begins.
func (s *PluginsService) executePluginMetadataMutation(ctx context.Context, principal Principal, project ResolvedProject, operation, key string, normalized normalizedPluginMetadataMutation, precheck func(context.Context) error, mutate func(context.Context, pgx.Tx) (PluginMetadataReceiptResult, error)) (PluginMetadataMutationOutput, error) {
	payload, err := json.Marshal(normalized)
	if err != nil {
		return PluginMetadataMutationOutput{}, pluginMetadataMutationInvalid("invalid_request", "The plugin request could not be normalized.")
	}
	digest := sha256.Sum256(append([]byte("platform-mcp-plugin-metadata-v1\x00"+operation+"\x00"), payload...))
	inputHash := hex.EncodeToString(digest[:])
	// A retry of a change that already committed replays its receipt without
	// spending the allowance; see executeChargedMutationReceipt.
	charge := func(ctx context.Context) error {
		if precheck != nil {
			if err := precheck(ctx); err != nil {
				return err
			}
		}
		return s.chargePluginMetadataMutation(ctx, principal)
	}
	receipt, err := executeChargedMutationReceipt(ctx, charge, mutationReceiptExecution[PluginMetadataReceiptResult]{
		DB: s.db, Now: s.now, Principal: principal, Project: project, Operation: operation,
		IdempotencyKey: key, InputHash: inputHash, Label: "plugin metadata",
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
				Slug:    "",
				Cause:   ErrPluginMetadataMutationConflict,
			}
		},
		Unavailable:    pluginMetadataMutationUnavailable,
		ValidateReplay: validPluginMetadataReceiptPayload,
		EncodeResult:   encodePluginMetadataReceiptResult,
		Mutate:         mutate,
	})
	if err != nil {
		return PluginMetadataMutationOutput{}, err
	}
	return s.finishPluginMetadataMutation(ctx, principal, project, receipt)
}

func (s *PluginsService) finishPluginMetadataMutation(ctx context.Context, principal Principal, project ResolvedProject, receipt OperationReceipt) (PluginMetadataMutationOutput, error) {
	var result PluginMetadataReceiptResult
	if err := json.Unmarshal(receipt.ResultPayload, &result); err != nil {
		return PluginMetadataMutationOutput{}, pluginMetadataMutationUnavailable(err)
	}
	output := PluginMetadataMutationOutput{PluginMetadataReceiptResult: result, PublishSignal: "not_requested", Receipt: riskMutationToolReceipt(receipt)}
	if !receipt.Replayed && result.PublicationRequest != string(plugindelivery.ProjectPublicationEnqueued) {
		switch {
		case s.publisher == nil:
			output.PublishSignal = "unavailable"
		case plugindelivery.SignalPluginPublishAfterRequest(ctx, s.publisher, plugindelivery.ProjectPublicationRequestOutcome(result.PublicationRequest), project.ID, principal.UserID) != nil:
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
			Assignments: *inventory.Assignments, Publication: inventory.Publication, AutoSyncEnabled: inventory.AutoSyncEnabled,
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
	switch result.ResultCategory {
	case pluginMetadataCreated, pluginMetadataRenamed, pluginMetadataAutoSyncTurnedOn, pluginMetadataAutoSyncTurnedOff:
	default:
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
	case errors.Is(err, plugindelivery.ErrPluginSlugInvalid), errors.Is(err, plugindelivery.ErrPluginSlugTooLong), errors.Is(err, plugindelivery.ErrPluginSlugUnavailable), errors.Is(err, plugindelivery.ErrPluginNameWithoutSlug), errors.Is(err, plugindelivery.ErrPluginNameEmpty):
		return pluginMetadataMutationInvalid("invalid_request", err.Error()+".")
	case errors.Is(err, plugindelivery.ErrPluginSlugConflict):
		return &PluginMetadataMutationError{Code: pluginMetadataCodeSlugTaken, Message: "A plugin with this slug already exists in this project. List the project's plugins again, then either use that plugin or choose a different slug.", Slug: "", Cause: fmt.Errorf("%w: %w", ErrPluginMetadataMutationConflict, err)}
	case errors.Is(err, plugindelivery.ErrPluginHasUnsyncedChanges):
		return &PluginMetadataMutationError{Code: pluginMetadataCodeUnsyncedChanges, Message: "Auto sync is still on: this plugin has changes that haven't been synced yet, and turning it off now would hold them back with nothing saying so. Sync them first with republish_plugin, which republishes every plugin in the project once the user confirms. When get_plugin reports publication_evidence.fresh=true, turn auto sync off.", Slug: "", Cause: err}
	case errors.Is(err, plugindelivery.ErrPluginMetadataTargetNotFound):
		return ErrPluginNotFound
	default:
		return err
	}
}

func pluginMetadataMutationInvalid(code, message string) error {
	return &PluginMetadataMutationError{Code: code, Message: message, Slug: "", Cause: ErrPluginMetadataMutationInvalid}
}

// pluginMetadataUnavailableMessage refuses a plugin metadata write the server
// was composed without.
const pluginMetadataUnavailableMessage = "Creating and renaming plugins is not available on this server."

// pluginAutoSyncUnavailableMessage refuses set_plugin_auto_sync on a server
// composed without the turn-off check, even where creating and renaming work.
const pluginAutoSyncUnavailableMessage = "Turning a plugin's auto sync on or off is not available on this server."

func pluginMetadataMutationUnavailable(cause error) error {
	if cause == nil {
		return &PluginMetadataMutationError{Code: unavailableCode, Message: pluginMetadataUnavailableMessage, Slug: "", Cause: ErrPluginMetadataMutationUnavailable}
	}
	return &PluginMetadataMutationError{Code: unavailableCode, Message: "Creating and changing plugins is temporarily unavailable.", Slug: "", Cause: fmt.Errorf("%w: %w", ErrPluginMetadataMutationUnavailable, cause)}
}
