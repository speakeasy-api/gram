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

const maxAssistantBuiltinGrantBackfillResults = 100

const assistantBuiltinGrantsBackfillSideEffects = "Grant every live agent-backed assistant in this organization explicit permanent mcp:connect policy grants for its built-in toolsets (assistants for every assistant; platform and speakeasy-slack as well for managed assistants). Existing permanent grants are left intact; matching temporary grants are promoted to permanent, and unbound assistants are skipped. Does not provision identities or change runtimes, agent names, or assistant configuration. The customer's audit log attributes changes to the Speakeasy team. Safe to run again."

// BackfillAssistantBuiltinGrantsInput selects one exact organization.
type BackfillAssistantBuiltinGrantsInput struct {
	// OrganizationID is a canonical ID, never a name or slug.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
}

type assistantBuiltinGrantBackfillResult struct {
	AssistantID string `json:"assistant_id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Outcome     string `json:"outcome"`
	GrantsAdded int64  `json:"grants_added"`
}

type assistantBuiltinGrantBackfillReceipt struct {
	PossiblyIncomplete bool                                  `json:"possibly_incomplete"`
	OrganizationID     string                                `json:"organization_id"`
	Granted            int64                                 `json:"granted"`
	AlreadyGranted     int64                                 `json:"already_granted"`
	Failed             int64                                 `json:"failed"`
	Results            []assistantBuiltinGrantBackfillResult `json:"results"`
	SideEffects        string                                `json:"side_effects"`
}

// assistantBuiltinGrantsBackfillWriter adds explicit built-in Connect grants
// without changing identity bindings or runtimes.
type assistantBuiltinGrantsBackfillWriter struct {
	runner *writeRunner
	audit  *audit.Logger
	writes WriteConfig
}

func (w *assistantBuiltinGrantsBackfillWriter) apply(ctx context.Context, input BackfillAssistantBuiltinGrantsInput) (assistantBuiltinGrantBackfillReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationBackfillAssistantBuiltinGrants)
	if err != nil {
		return assistantBuiltinGrantBackfillReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return assistantBuiltinGrantBackfillReceipt{}, err
	}
	return runWrite(ctx, w.runner, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (assistantBuiltinGrantBackfillReceipt, error) {
		ctx, _, _ = staffMutation(ctx, authority)
		run, err := admin.BackfillAssistantBuiltinGrantsTx(ctx, tx, w.runner.logger, w.audit, input.OrganizationID)
		switch {
		case errors.Is(err, admin.ErrAssistantMigrationOrganizationNotFound):
			return assistantBuiltinGrantBackfillReceipt{}, errors.New("no organization with that ID")
		case err != nil:
			return assistantBuiltinGrantBackfillReceipt{}, fmt.Errorf("backfill assistant builtin grants: %w", err)
		}
		return assistantBuiltinGrantBackfillReceiptFromRun(run), nil
	})
}

// assistantBuiltinGrantBackfillReceiptFromRun bounds details, not the organization's mutation.
func assistantBuiltinGrantBackfillReceiptFromRun(run *gen.AdminAssistantBuiltinGrantBackfillRun) assistantBuiltinGrantBackfillReceipt {
	results := run.Results[:min(len(run.Results), maxAssistantBuiltinGrantBackfillResults)]
	receipt := assistantBuiltinGrantBackfillReceipt{
		OrganizationID: run.OrganizationID, Granted: run.Granted, AlreadyGranted: run.AlreadyGranted, Failed: run.Failed,
		Results: make([]assistantBuiltinGrantBackfillResult, 0, len(results)), SideEffects: assistantBuiltinGrantsBackfillSideEffects,
		PossiblyIncomplete: len(run.Results) > maxAssistantBuiltinGrantBackfillResults,
	}
	for _, result := range results {
		receipt.Results = append(receipt.Results, assistantBuiltinGrantBackfillResult{
			AssistantID: result.AssistantID,
			ProjectID:   result.ProjectID,
			Name:        result.Name,
			Outcome:     result.Outcome,
			GrantsAdded: result.GrantsAdded,
		})
	}
	return receipt
}

func (w *assistantBuiltinGrantsBackfillWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{
		Name:        string(OperationBackfillAssistantBuiltinGrants),
		Title:       "Backfill Organization Assistant Built-in Grants",
		Description: "Backfill explicit mcp:connect policy grants for built-in toolsets on live agent-backed assistants for one exact canonical organization ID. " + assistantBuiltinGrantsBackfillSideEffects + " Operates on all live agent-backed assistants in the organization. Returns only the first 100 result details, with possibly_incomplete true when more exist; there is no pagination. Counts cover the full run. Read list_organization_assistants first and show the user which assistants are in scope.",
	}, w.apply)
}
