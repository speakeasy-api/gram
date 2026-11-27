//nolint:exhaustruct_v5 // MCP manifests and failure results use documented zero values.
package adminmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/encryption"
)

const maxAssistantSlackMigrationResults = 100

const assistantSlackMigrationSideEffects = "Migrate live agent-backed assistants in this organization from per-assistant Slack toolsets and environment-backed Slack triggers to the built-in assistant-slack toolset backed by encrypted slack_app auth clients. Creates or reuses a slack_app auth client, grants the assistant's agent permanent mcp:connect on assistant-slack and mcp:auth on the client, repoints legacy Slack triggers to the client (clearing environment_id), and detaches (and soft-deletes if unreferenced) Slack-only per-assistant toolsets. Never deletes project environments or returns secret values. Assistants without a live dedicated agent are reported as needs_identity and left unchanged. The customer's audit log attributes changes to the Speakeasy team. Safe to run again."

// MigrateAssistantSlackInput selects one exact organization.
type MigrateAssistantSlackInput struct {
	// OrganizationID is a canonical ID, never a name or slug.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
}

type assistantSlackMigrationResult struct {
	AssistantID       string  `json:"assistant_id"`
	ProjectID         string  `json:"project_id"`
	Name              string  `json:"name"`
	Outcome           string  `json:"outcome"`
	AuthClientID      *string `json:"auth_client_id,omitempty"`
	TriggersRepointed int64   `json:"triggers_repointed"`
	ToolsetsDetached  int64   `json:"toolsets_detached"`
	ToolsetsDeleted   int64   `json:"toolsets_deleted"`
}

type assistantSlackMigrationReceipt struct {
	PossiblyIncomplete bool                            `json:"possibly_incomplete"`
	OrganizationID     string                          `json:"organization_id"`
	Migrated           int64                           `json:"migrated"`
	AlreadyMigrated    int64                           `json:"already_migrated"`
	NoSlack            int64                           `json:"no_slack"`
	NeedsIdentity      int64                           `json:"needs_identity"`
	Failed             int64                           `json:"failed"`
	Results            []assistantSlackMigrationResult `json:"results"`
	SideEffects        string                          `json:"side_effects"`
}

type assistantSlackMigrationWriter struct {
	runner *writeRunner
	audit  *audit.Logger
	enc    *encryption.Client
	writes WriteConfig
}

func (w *assistantSlackMigrationWriter) apply(ctx context.Context, input MigrateAssistantSlackInput) (assistantSlackMigrationReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationMigrateAssistantSlack)
	if err != nil {
		return assistantSlackMigrationReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return assistantSlackMigrationReceipt{}, err
	}
	return runWrite(ctx, w.runner, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (assistantSlackMigrationReceipt, error) {
		ctx, _, _ = staffMutation(ctx, authority)
		run, err := admin.MigrateAssistantSlackTx(ctx, tx, w.runner.logger, otel.GetTracerProvider(), w.audit, w.enc, input.OrganizationID)
		switch {
		case errors.Is(err, admin.ErrAssistantMigrationOrganizationNotFound):
			return assistantSlackMigrationReceipt{}, errors.New("no organization with that ID")
		case errors.Is(err, admin.ErrAssistantSlackMigrationUnavailable):
			return assistantSlackMigrationReceipt{}, errors.New("application encryption is not configured")
		case err != nil:
			return assistantSlackMigrationReceipt{}, fmt.Errorf("migrate assistant Slack: %w", err)
		}
		return assistantSlackMigrationReceiptFromRun(run), nil
	})
}

func assistantSlackMigrationReceiptFromRun(run *gen.AdminAssistantSlackMigrationRun) assistantSlackMigrationReceipt {
	results := run.Results[:min(len(run.Results), maxAssistantSlackMigrationResults)]
	receipt := assistantSlackMigrationReceipt{
		OrganizationID:     run.OrganizationID,
		Migrated:           run.Migrated,
		AlreadyMigrated:    run.AlreadyMigrated,
		NoSlack:            run.NoSlack,
		NeedsIdentity:      run.NeedsIdentity,
		Failed:             run.Failed,
		Results:            make([]assistantSlackMigrationResult, 0, len(results)),
		SideEffects:        assistantSlackMigrationSideEffects,
		PossiblyIncomplete: len(run.Results) > maxAssistantSlackMigrationResults,
	}
	for _, result := range results {
		receipt.Results = append(receipt.Results, assistantSlackMigrationResult{
			AssistantID:       result.AssistantID,
			ProjectID:         result.ProjectID,
			Name:              result.Name,
			Outcome:           result.Outcome,
			AuthClientID:      result.AuthClientID,
			TriggersRepointed: result.TriggersRepointed,
			ToolsetsDetached:  result.ToolsetsDetached,
			ToolsetsDeleted:   result.ToolsetsDeleted,
		})
	}
	return receipt
}

func (w *assistantSlackMigrationWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{
		Name:        string(OperationMigrateAssistantSlack),
		Title:       "Migrate Organization Assistant Slack",
		Description: "Migrate legacy per-assistant Slack toolsets and environment-backed Slack triggers to the built-in assistant-slack toolset and slack_app typed auth clients for one exact canonical organization ID. " + assistantSlackMigrationSideEffects + " Operates on all live assistants in the organization. Returns only the first 100 result details, with possibly_incomplete true when more exist; there is no pagination. Counts cover the full run. Read list_organization_assistants first and show the user which assistants are in scope.",
	}, w.apply)
}
