//nolint:exhaustruct // MCP manifests and failure results use documented zero values.
package adminmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
)

const assistantIdentityMigrationSideEffects = "Each migrated assistant gets a dedicated agent owned by its creator, starting with access to every MCP server and skill in its project, administration of the assistant, and project inference only when the creator has it; its triggers get workload identities. Its turns then run as that agent instead of as its owner. Assistants whose creator is no longer a member are left unchanged and reported as needs_owner. The customer's audit log shows the changes as made by the Speakeasy team. Safe to run again."

// MigrateAssistantIdentitiesInput selects one exact organization.
type MigrateAssistantIdentitiesInput struct {
	// OrganizationID is a canonical ID, never a name or slug.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
}

type assistantIdentityMigrationResult struct {
	AssistantID string `json:"assistant_id"`
	ProjectID   string `json:"project_id"`
	Outcome     string `json:"outcome"`
}

type assistantIdentityMigrationReceipt struct {
	OrganizationID  string                             `json:"organization_id"`
	Migrated        int64                              `json:"migrated"`
	AlreadyMigrated int64                              `json:"already_migrated"`
	NeedsOwner      int64                              `json:"needs_owner"`
	Failed          int64                              `json:"failed"`
	Results         []assistantIdentityMigrationResult `json:"results"`
	SideEffects     string                             `json:"side_effects"`
}

// assistantIdentityMigrationWriter migrates one organization's assistants to
// agent identity. Provisioning skips assistants that already have an agent, so
// a repeated call changes nothing more.
type assistantIdentityMigrationWriter struct {
	runner     *writeRunner
	identities *assistantidentity.Service
	writes     WriteConfig
}

func (w *assistantIdentityMigrationWriter) apply(ctx context.Context, input MigrateAssistantIdentitiesInput) (assistantIdentityMigrationReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationMigrateAssistantIdentities)
	if err != nil {
		return assistantIdentityMigrationReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return assistantIdentityMigrationReceipt{}, err
	}
	return runWrite(ctx, w.runner, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (assistantIdentityMigrationReceipt, error) {
		ctx, actor, displayName := staffMutation(ctx, authority)
		run, err := admin.MigrateAssistantIdentitiesTx(ctx, tx, w.runner.logger, w.identities, input.OrganizationID, &assistantidentity.StaffActor{Principal: actor, DisplayName: displayName})
		switch {
		case errors.Is(err, admin.ErrAssistantMigrationOrganizationNotFound):
			return assistantIdentityMigrationReceipt{}, errors.New("no organization with that ID")
		case err != nil:
			return assistantIdentityMigrationReceipt{}, fmt.Errorf("migrate assistant identities: %w", err)
		}
		receipt := assistantIdentityMigrationReceipt{
			OrganizationID: run.OrganizationID, Migrated: run.Migrated, AlreadyMigrated: run.AlreadyMigrated, NeedsOwner: run.NeedsOwner, Failed: run.Failed,
			Results: make([]assistantIdentityMigrationResult, 0, len(run.Results)), SideEffects: assistantIdentityMigrationSideEffects,
		}
		for _, result := range run.Results {
			receipt.Results = append(receipt.Results, assistantIdentityMigrationResult{AssistantID: result.AssistantID, ProjectID: result.ProjectID, Outcome: result.Outcome})
		}
		return receipt, nil
	})
}

func (w *assistantIdentityMigrationWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationMigrateAssistantIdentities), Title: "Migrate Organization Assistants to Agent Identity", Description: "Give every assistant of one exact canonical organization ID that has no dedicated agent its own agent. " + assistantIdentityMigrationSideEffects + " Read list_organization_assistants first and show the user which assistants would change."}, w.apply)
}
