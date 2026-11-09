//nolint:exhaustruct // MCP manifests and failure results use documented zero values.
package adminmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/businessmemory"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/chatanalysis"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

const chatAnalysisSideEffects = "Enabling analysis or increasing its daily cap permits background LLM spend. The cap limits evaluations, not currency cost. This change does not run analysis immediately; it records a tenant audit event."

var errChatAnalysisNoChange = errors.New("chat analysis settings already match the requested change")

// ChatAnalysisInput is one exact judge settings change.
type ChatAnalysisInput struct {
	// OrganizationID selects an exact canonical organisation, never a slug.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations"`

	// Judge identifies one supported analysis.
	Judge string `json:"judge" jsonschema:"Analysis judge: work_units or business_memory"`

	// Enabled permits background evaluations.
	Enabled bool `json:"enabled" jsonschema:"Desired enabled state"`

	// DailyCap bounds evaluations per day, not currency spend.
	DailyCap int `json:"daily_cap" jsonschema:"Daily evaluation cap from 0 to 10000"`
}

type chatAnalysisState struct {
	OrganizationID string
	Name           string
	Slug           string
	Settings       chatanalysis.JudgeSettings
}

type chatAnalysisReceipt struct {
	OrganizationID string                     `json:"organization_id"`
	Name           string                     `json:"name"`
	Slug           string                     `json:"slug"`
	Before         chatanalysis.JudgeSettings `json:"before"`
	After          chatanalysis.JudgeSettings `json:"after"`
	SideEffects    string                     `json:"side_effects"`
}

type chatAnalysisWriter struct {
	runner *writeRunner
	audit  *audit.Logger
	writes WriteConfig
}

func validChatAnalysisChange(input ChatAnalysisInput) bool {
	return (input.Judge == analysis.WorkUnitsJudgeName || input.Judge == businessmemory.JudgeName) && input.DailyCap >= 0 && input.DailyCap <= chatanalysis.MaxDailyCap
}

func (w *chatAnalysisWriter) readState(ctx context.Context, tx pgx.Tx, organizationID, judge string) (chatAnalysisState, error) {
	org, err := orgrepo.New(tx).LockOrganizationForAdminConfiguration(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && org.ID != organizationID) {
		return chatAnalysisState{}, ErrStaleState
	}
	if err != nil {
		return chatAnalysisState{}, fmt.Errorf("lock analysis target: %w", err)
	}
	settings, err := chatanalysis.LockAndLoadSettingsTx(ctx, tx, organizationID, judge)
	if err != nil {
		return chatAnalysisState{}, fmt.Errorf("read analysis target settings: %w", err)
	}
	return chatAnalysisState{OrganizationID: org.ID, Name: org.Name, Slug: org.Slug, Settings: settings}, nil
}

func (w *chatAnalysisWriter) apply(ctx context.Context, input ChatAnalysisInput) (chatAnalysisReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationSetChatAnalysisSettings)
	if err != nil {
		return chatAnalysisReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return chatAnalysisReceipt{}, err
	}
	if !validChatAnalysisChange(input) {
		return chatAnalysisReceipt{}, errors.New("provide a supported judge and daily cap from 0 to 10000")
	}
	return runWrite(ctx, w.runner, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (chatAnalysisReceipt, error) {
		state, err := w.readState(ctx, tx, input.OrganizationID, input.Judge)
		if err != nil {
			return chatAnalysisReceipt{}, err
		}
		if state.Settings.Enabled == input.Enabled && state.Settings.DailyCap == input.DailyCap {
			return chatAnalysisReceipt{}, errChatAnalysisNoChange
		}
		ctx, actor, name := staffMutation(ctx, authority)
		if _, err := chatanalysis.UpsertSettingsTx(ctx, tx, w.audit, input.OrganizationID, input.Judge, input.Enabled, input.DailyCap, actor, name); err != nil {
			return chatAnalysisReceipt{}, fmt.Errorf("apply analysis settings: %w", err)
		}
		after := chatanalysis.JudgeSettings{Judge: input.Judge, Enabled: input.Enabled, DailyCap: input.DailyCap, IsDefault: false}
		return chatAnalysisReceipt{OrganizationID: state.OrganizationID, Name: state.Name, Slug: state.Slug, Before: state.Settings, After: after, SideEffects: chatAnalysisSideEffects}, nil
	})
}

func (w *chatAnalysisWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationSetChatAnalysisSettings), Title: "Set Chat Analysis Settings", Description: "Set one exact organization's judge enabled state and daily evaluation cap. Does not run analysis. Enabling or increasing the cap permits background LLM spend, not a currency budget."}, w.apply)
}
