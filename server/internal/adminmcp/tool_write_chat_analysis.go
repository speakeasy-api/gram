//nolint:exhaustruct // MCP manifests and failure results use documented zero values.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/businessmemory"
	"github.com/speakeasy-api/gram/server/internal/chat/analysis"
	"github.com/speakeasy-api/gram/server/internal/chatanalysis"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

const chatAnalysisSideEffects = "Enabling analysis or increasing its daily cap permits background LLM spend. The cap limits evaluations, not currency cost. This change does not run analysis immediately; it records a tenant audit event."

var errChatAnalysisNoChange = errors.New("chat analysis settings already match the requested change")

type PrepareChatAnalysisInput struct {
	// OrganizationID selects an exact canonical organisation, never a slug.
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations"`

	// Judge identifies one supported analysis.
	Judge string `json:"judge" jsonschema:"Analysis judge: work_units or business_memory"`

	// Enabled permits background evaluations.
	Enabled bool `json:"enabled" jsonschema:"Desired enabled state"`

	// DailyCap bounds evaluations per day, not currency spend.
	DailyCap int `json:"daily_cap" jsonschema:"Daily evaluation cap from 0 to 10000"`

	// RetryKey deduplicates this exact stored proposal.
	RetryKey string `json:"retry_key" jsonschema:"Unique retry key for this exact change, up to 128 characters"`
}

type chatAnalysisChange struct {
	Judge    string `json:"judge"`
	Enabled  bool   `json:"enabled"`
	DailyCap int    `json:"daily_cap"`
}

type chatAnalysisState struct {
	OrganizationID string                     `json:"organization_id"`
	Name           string                     `json:"name"`
	Slug           string                     `json:"slug"`
	Settings       chatanalysis.JudgeSettings `json:"settings"`
}

type chatAnalysisPreview struct {
	OrganizationID string                     `json:"organization_id"`
	Name           string                     `json:"name"`
	Slug           string                     `json:"slug"`
	Before         chatanalysis.JudgeSettings `json:"before"`
	After          chatanalysis.JudgeSettings `json:"after"`
	SideEffects    string                     `json:"side_effects"`
}

type chatAnalysisWriter struct {
	store   *proposalStore
	audit   *audit.Logger
	writes  WriteConfig
	baseURL string
	now     func() time.Time
}

func validChatAnalysisChange(change chatAnalysisChange) bool {
	return (change.Judge == analysis.WorkUnitsJudgeName || change.Judge == businessmemory.JudgeName) && change.DailyCap >= 0 && change.DailyCap <= chatanalysis.MaxDailyCap
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

func (w *chatAnalysisWriter) expectedState(ctx context.Context, tx pgx.Tx, proposal Proposal) (chatAnalysisChange, error) {
	if proposal.Operation != OperationSetChatAnalysisSettings || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || proposal.Target.OrganizationID == "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceKind != "" || proposal.Target.ResourceID != "" {
		return chatAnalysisChange{}, ErrProposalInvalidated
	}
	var change chatAnalysisChange
	if json.Unmarshal(proposal.Arguments, &change) != nil || !validChatAnalysisChange(change) {
		return chatAnalysisChange{}, ErrProposalInvalidated
	}
	state, err := w.readState(ctx, tx, proposal.Target.OrganizationID, change.Judge)
	if err != nil {
		return chatAnalysisChange{}, err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return chatAnalysisChange{}, fmt.Errorf("encode analysis state: %w", err)
	}
	digest, err := stateDigest(encoded)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) {
		return chatAnalysisChange{}, ErrStaleState
	}
	return change, nil
}

func (w *chatAnalysisWriter) revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	_, err := w.expectedState(ctx, tx, proposal)
	return err
}

func (w *chatAnalysisWriter) prepare(ctx context.Context, input PrepareChatAnalysisInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationSetChatAnalysisSettings)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := checkPrepareTarget(input.OrganizationID, input.RetryKey); err != nil {
		return ProposalOutput{}, err
	}
	change := chatAnalysisChange{Judge: input.Judge, Enabled: input.Enabled, DailyCap: input.DailyCap}
	if !validChatAnalysisChange(change) {
		return ProposalOutput{}, errors.New("provide a supported judge and daily cap from 0 to 10000")
	}
	owner, err := ownerFromAuthority(authority)
	if err != nil {
		return ProposalOutput{}, err
	}
	tx, err := w.store.db.Begin(ctx)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("begin analysis preparation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(ctx)) })
	state, err := w.readState(ctx, tx, input.OrganizationID, input.Judge)
	if err != nil {
		return ProposalOutput{}, err
	}
	if state.Settings.Enabled == input.Enabled && state.Settings.DailyCap == input.DailyCap {
		return ProposalOutput{}, errChatAnalysisNoChange
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalOutput{}, fmt.Errorf("finish analysis preparation: %w", err)
	}
	args, err := json.Marshal(change)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode analysis change: %w", err)
	}
	expected, err := json.Marshal(state)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode analysis snapshot: %w", err)
	}
	after := chatanalysis.JudgeSettings{Judge: input.Judge, Enabled: input.Enabled, DailyCap: input.DailyCap, IsDefault: false}
	preview, err := json.Marshal(chatAnalysisPreview{OrganizationID: state.OrganizationID, Name: state.Name, Slug: state.Slug, Before: state.Settings, After: after, SideEffects: chatAnalysisSideEffects})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode analysis preview: %w", err)
	}
	proposal, replay, err := w.store.Create(ctx, owner, NewProposal{Operation: OperationSetChatAnalysisSettings, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: state.OrganizationID}, IdempotencyKey: input.RetryKey, Arguments: args, ExpectedState: expected, Preview: preview}, w.now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, w.baseURL), nil
}

func (w *chatAnalysisWriter) view(proposal Proposal) (proposalView, error) {
	var preview chatAnalysisPreview
	if json.Unmarshal(proposal.Preview, &preview) != nil || proposal.PlatformGlobal || preview.OrganizationID == "" || preview.OrganizationID != proposal.Target.OrganizationID || preview.Before.Judge != preview.After.Judge || !validChatAnalysisChange(chatAnalysisChange{Judge: preview.After.Judge, Enabled: preview.After.Enabled, DailyCap: preview.After.DailyCap}) {
		return proposalView{}, ErrProposalInvalidated
	}
	return proposalView{
		Summary:        "Change " + preview.After.Judge + " chat analysis settings",
		OrganizationID: preview.OrganizationID, OrganizationName: preview.Name, OrganizationSlug: preview.Slug,
		Changes: []proposalViewChange{
			{Setting: "Enabled", Before: enabledLabel(preview.Before.Enabled), After: enabledLabel(preview.After.Enabled)},
			{Setting: "Daily evaluation cap", Before: strconv.Itoa(preview.Before.DailyCap), After: strconv.Itoa(preview.After.DailyCap)},
			{Setting: "Stored settings", Before: enabledLabel(!preview.Before.IsDefault), After: "On"},
		},
		SideEffects: chatAnalysisSideEffects,
	}, nil
}

func (w *chatAnalysisWriter) execution(authority writeAuthority) ProposalExecution {
	return ProposalExecution{Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
		change, err := w.expectedState(ctx, tx, proposal)
		if err != nil {
			return "", nil, err
		}
		ctx, actor, name := staffMutation(ctx, authority)
		if _, err := chatanalysis.UpsertSettingsTx(ctx, tx, w.audit, proposal.Target.OrganizationID, change.Judge, change.Enabled, change.DailyCap, actor, name); err != nil {
			return "", nil, fmt.Errorf("apply analysis settings: %w", err)
		}
		result, err := json.Marshal(change)
		if err != nil {
			return "", nil, fmt.Errorf("encode analysis receipt: %w", err)
		}
		return "succeeded", result, nil
	}}
}

func (w *chatAnalysisWriter) registerPrepare(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "prepare_set_organization_chat_analysis_settings", Title: "Prepare Chat Analysis Settings", Description: "Prepare one exact organization's judge enabled state and daily evaluation cap. Requires separate same-staff browser approval and admin:write; does not apply settings or run analysis. Enabling or increasing the cap permits background LLM spend, not a currency budget."}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareChatAnalysisInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := w.prepare(ctx, input)
		return nil, out, err
	})
}
