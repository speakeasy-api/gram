//nolint:exhaustruct_v5 // MCP manifests and failure results use documented zero values.
package adminmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
)

const maxAssistantAgentNameRepairResults = 100

const assistantAgentNameRepairSideEffects = "Replace legacy generated Assistant <assistant ID> agent display names with the linked assistant name in this organization only. Custom agent names are left unchanged. Conflicting names are reported and left unchanged. Does not provision identities or change permissions, runtime selection, or assistant configuration. The customer's audit log attributes changes to the Speakeasy team. Safe to run again."

// RepairAssistantAgentNamesInput selects one exact organization.
type RepairAssistantAgentNamesInput struct {
	// OrganizationID is a canonical ID, never a name or slug.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
}

type assistantAgentNameRepairResult struct {
	AssistantID string `json:"assistant_id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Outcome     string `json:"outcome"`
}

type assistantAgentNameRepairReceipt struct {
	PossiblyIncomplete bool                             `json:"possibly_incomplete"`
	OrganizationID     string                           `json:"organization_id"`
	Repaired           int64                            `json:"repaired"`
	Conflicts          int64                            `json:"conflicts"`
	Failed             int64                            `json:"failed"`
	Results            []assistantAgentNameRepairResult `json:"results"`
	SideEffects        string                           `json:"side_effects"`
}

// assistantAgentNameRepairWriter repairs names without changing identity or runtime.
type assistantAgentNameRepairWriter struct {
	runner *writeRunner
	audit  *audit.Logger
	writes WriteConfig
}

func (w *assistantAgentNameRepairWriter) apply(ctx context.Context, input RepairAssistantAgentNamesInput) (assistantAgentNameRepairReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationRepairAssistantAgentNames)
	if err != nil {
		return assistantAgentNameRepairReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return assistantAgentNameRepairReceipt{}, err
	}
	return runWrite(ctx, w.runner, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (assistantAgentNameRepairReceipt, error) {
		ctx, _, _ = staffMutation(ctx, authority)
		run, err := admin.RepairAssistantAgentNamesTx(ctx, tx, w.runner.logger, w.audit, input.OrganizationID)
		switch {
		case errors.Is(err, admin.ErrAssistantMigrationOrganizationNotFound):
			return assistantAgentNameRepairReceipt{}, errors.New("no organization with that ID")
		case err != nil:
			return assistantAgentNameRepairReceipt{}, fmt.Errorf("repair assistant agent names: %w", err)
		}
		return assistantAgentNameRepairReceiptFromRun(run), nil
	})
}

// assistantAgentNameRepairReceiptFromRun bounds details, not the organization's mutation.
func assistantAgentNameRepairReceiptFromRun(run *gen.AdminAssistantAgentNameRepairRun) assistantAgentNameRepairReceipt {
	results := run.Results[:min(len(run.Results), maxAssistantAgentNameRepairResults)]
	receipt := assistantAgentNameRepairReceipt{
		OrganizationID: run.OrganizationID, Repaired: run.Repaired, Conflicts: run.Conflicts, Failed: run.Failed,
		Results: make([]assistantAgentNameRepairResult, 0, len(results)), SideEffects: assistantAgentNameRepairSideEffects,
		PossiblyIncomplete: len(run.Results) > maxAssistantAgentNameRepairResults,
	}
	for _, result := range results {
		receipt.Results = append(receipt.Results, assistantAgentNameRepairResult{AssistantID: result.AssistantID, ProjectID: result.ProjectID, Name: result.Name, Outcome: result.Outcome})
	}
	return receipt
}

func (w *assistantAgentNameRepairWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationRepairAssistantAgentNames), Title: "Repair Organization Assistant Agent Names", Description: "Repair stale assistant agent display names for one exact canonical organization ID. " + assistantAgentNameRepairSideEffects + " Operates on all eligible assistants in the organization. Returns only the first 100 result details, with possibly_incomplete true when more exist; there is no pagination. Counts cover the full run. Read list_organization_assistants first and show the user which assistants would change."}, w.apply)
}
