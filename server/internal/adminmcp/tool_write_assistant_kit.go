//nolint:exhaustruct // MCP manifests and failure results use documented zero values.
package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	"github.com/speakeasy-api/gram/server/internal/audit"
)

const assistantKitMigrationSideEffects = "Each switched assistant runs its next turns on the Kit runtime, and its previous runtimes are retired and stopped by the runtime janitor within the hour. Only an assistant with a usable dedicated agent, a shared user-session issuer in its project, no work in flight, and no runtime inside its warm window is switched; the others are left unchanged and reported as needs_identity, ineligible, or busy. The customer's audit log shows each switch as made by the Speakeasy team. Safe to run again."

// MigrateAssistantsToKitInput selects one exact organization and, optionally,
// some of its assistants.
type MigrateAssistantsToKitInput struct {
	// OrganizationID is a canonical ID, never a name or slug.
	OrganizationID string   `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
	AssistantIDs   []string `json:"assistant_ids,omitempty" jsonschema:"Exact assistant IDs from list_organization_assistants. Omit to move every live assistant of the organization"`
}

type assistantKitMigrationResult struct {
	AssistantID string `json:"assistant_id"`
	ProjectID   string `json:"project_id"`
	Outcome     string `json:"outcome"`
}

type assistantKitMigrationReceipt struct {
	OrganizationID string                        `json:"organization_id"`
	Switched       int64                         `json:"switched"`
	AlreadyKit     int64                         `json:"already_kit"`
	NeedsIdentity  int64                         `json:"needs_identity"`
	Ineligible     int64                         `json:"ineligible"`
	Busy           int64                         `json:"busy"`
	Failed         int64                         `json:"failed"`
	Results        []assistantKitMigrationResult `json:"results"`
	SideEffects    string                        `json:"side_effects"`
}

// assistantKitMigrationWriter moves assistants of one organization to the Kit
// runtime. Assistants already on Kit are reported and left alone, so a repeated
// call changes nothing more.
type assistantKitMigrationWriter struct {
	runner *writeRunner
	audit  *audit.Logger
	writes WriteConfig
	now    func() time.Time
}

func (w *assistantKitMigrationWriter) apply(ctx context.Context, input MigrateAssistantsToKitInput) (assistantKitMigrationReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationMigrateAssistantsToKit)
	if err != nil {
		return assistantKitMigrationReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return assistantKitMigrationReceipt{}, err
	}
	return runWrite(ctx, w.runner, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (assistantKitMigrationReceipt, error) {
		ctx, actor, displayName := staffMutation(ctx, authority)
		run, err := admin.MigrateAssistantsToKitTx(ctx, tx, w.runner.logger, w.audit, input.OrganizationID, input.AssistantIDs, &assistantidentity.StaffActor{Principal: actor, DisplayName: displayName}, w.now())
		switch {
		case errors.Is(err, admin.ErrAssistantMigrationOrganizationNotFound):
			return assistantKitMigrationReceipt{}, errors.New("no organization with that ID")
		case errors.Is(err, admin.ErrAssistantNotInOrganization):
			return assistantKitMigrationReceipt{}, fmt.Errorf("select assistants: %w", err)
		case err != nil:
			return assistantKitMigrationReceipt{}, fmt.Errorf("migrate assistants to Kit: %w", err)
		}
		receipt := assistantKitMigrationReceipt{
			OrganizationID: run.OrganizationID, Switched: run.Switched, AlreadyKit: run.AlreadyKit, NeedsIdentity: run.NeedsIdentity, Ineligible: run.Ineligible, Busy: run.Busy, Failed: run.Failed,
			Results: make([]assistantKitMigrationResult, 0, len(run.Results)), SideEffects: assistantKitMigrationSideEffects,
		}
		for _, result := range run.Results {
			receipt.Results = append(receipt.Results, assistantKitMigrationResult{AssistantID: result.AssistantID, ProjectID: result.ProjectID, Outcome: result.Outcome})
		}
		return receipt, nil
	})
}

func (w *assistantKitMigrationWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationMigrateAssistantsToKit), Title: "Migrate Organization Assistants to Kit Runtime", Description: "Move assistants of one exact canonical organization ID from the default runtime to the Kit runtime. " + assistantKitMigrationSideEffects + " Read list_organization_assistants first and show the user which assistants would change."}, w.apply)
}
