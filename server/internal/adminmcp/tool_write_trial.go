//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/constants"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

const trialSideEffects = "Extends the enterprise trial end date and records a tenant audit event. No billing, model key, account type or feature change."

type PrepareTrialExtensionInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
	Days           int    `json:"days" jsonschema:"Calendar days to add to the current trial end date, 1 to 365"`
	RetryKey       string `json:"retry_key" jsonschema:"Unique retry key for this exact change (up to 128 characters)"`
}

type trialExtensionChange struct {
	Days int `json:"days"`
}

type trialLifecycle struct {
	Tier        string  `json:"tier"`
	EndsAt      string  `json:"ends_at"`
	ConvertedAt *string `json:"converted_at"`
	DemotedAt   *string `json:"demoted_at"`
}

// trialState is digested as the proposal's expected state, so another
// extension, a conversion or a demotion in between makes the proposal stale.
type trialState struct {
	OrganizationID string         `json:"organization_id"`
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Trial          trialLifecycle `json:"trial"`
}

type trialPreview struct {
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Days           int    `json:"days"`
	EndsAt         string `json:"ends_at"`
	NewEndsAt      string `json:"new_ends_at"`
	SideEffects    string `json:"side_effects"`
}

type trialReceipt struct {
	PreviousEndsAt string `json:"previous_ends_at"`
	EndsAt         string `json:"ends_at"`
}

func validTrialExtensionDays(days int) bool {
	return days >= constants.MinTrialExtensionDays && days <= constants.MaxTrialExtensionDays
}

func trialTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func optionalTrialTimestamp(t pgtype.Timestamptz) *string {
	if !t.Valid {
		return nil
	}
	value := trialTimestamp(t.Time)
	return &value
}

// trialWriter extends running trials. Extension adds to ends_at, so it is not
// idempotent by itself: the one-use receipt and the ends_at in the
// expected-state digest are what stop a second write extending again.
type trialWriter struct {
	store   *proposalStore
	audit   *audit.Logger
	writes  WriteConfig
	baseURL string
}

// readState locks the trial row that every trial lifecycle change locks. It
// returns admin.ErrTrialNotRunning when the organization has no trial row, and
// reports whether the trial is running now.
func (w *trialWriter) readState(ctx context.Context, tx pgx.Tx, organizationID string) (trialState, time.Time, bool, error) {
	org, err := orgrepo.New(tx).GetOrganizationMetadata(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && org.ID != organizationID) {
		return trialState{}, time.Time{}, false, ErrStaleState
	}
	if err != nil {
		return trialState{}, time.Time{}, false, fmt.Errorf("read trial target: %w", err)
	}
	trial, err := trialsrepo.New(tx).LockTrialLifecycle(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return trialState{}, time.Time{}, false, admin.ErrTrialNotRunning
	}
	if err != nil {
		return trialState{}, time.Time{}, false, fmt.Errorf("lock trial lifecycle: %w", err)
	}
	state := trialState{OrganizationID: org.ID, Name: org.Name, Slug: org.Slug, Trial: trialLifecycle{
		Tier: trial.Tier, EndsAt: trialTimestamp(trial.EndsAt.Time), ConvertedAt: optionalTrialTimestamp(trial.ConvertedAt), DemotedAt: optionalTrialTimestamp(trial.DemotedAt),
	}}
	running := !trial.ConvertedAt.Valid && !trial.DemotedAt.Valid && trial.EndsAt.Time.After(time.Now())
	return state, trial.EndsAt.Time, running, nil
}

func (w *trialWriter) expectedState(ctx context.Context, tx pgx.Tx, proposal Proposal) (trialExtensionChange, error) {
	if proposal.Operation != OperationExtendOrganizationTrial || proposal.SchemaVersion != 1 || proposal.PlatformGlobal || proposal.Target.OrganizationID == "" || proposal.Target.ProjectID.Valid || proposal.Target.ResourceID != "" || proposal.Target.ResourceKind != "" {
		return trialExtensionChange{}, ErrProposalInvalidated
	}
	var change trialExtensionChange
	if err := json.Unmarshal(proposal.Arguments, &change); err != nil || !validTrialExtensionDays(change.Days) {
		return trialExtensionChange{}, ErrProposalInvalidated
	}
	state, _, running, err := w.readState(ctx, tx, proposal.Target.OrganizationID)
	if errors.Is(err, admin.ErrTrialNotRunning) {
		return trialExtensionChange{}, ErrStaleState
	}
	if err != nil {
		return trialExtensionChange{}, err
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return trialExtensionChange{}, fmt.Errorf("encode expected trial state: %w", err)
	}
	digest, err := stateDigest(encoded)
	if err != nil || !digestsEqual(digest, proposal.ExpectedStateDigest) || !running {
		return trialExtensionChange{}, ErrStaleState
	}
	return change, nil
}

func (w *trialWriter) revalidate(ctx context.Context, tx pgx.Tx, proposal Proposal) error {
	_, err := w.expectedState(ctx, tx, proposal)
	return err
}

func (w *trialWriter) prepare(ctx context.Context, input PrepareTrialExtensionInput) (ProposalOutput, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationExtendOrganizationTrial)
	if err != nil {
		return ProposalOutput{}, err
	}
	owner, err := ownerFromAuthority(authority)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := checkPrepareTarget(input.OrganizationID, input.RetryKey); err != nil {
		return ProposalOutput{}, err
	}
	// Checked on the wide int, as admin.ExtendTrial does.
	if !validTrialExtensionDays(input.Days) {
		return ProposalOutput{}, fmt.Errorf("days must be between %d and %d", constants.MinTrialExtensionDays, constants.MaxTrialExtensionDays)
	}
	tx, err := w.store.db.Begin(ctx)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("begin trial proposal preparation: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	state, endsAt, running, err := w.readState(ctx, tx, input.OrganizationID)
	if err != nil {
		return ProposalOutput{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ProposalOutput{}, fmt.Errorf("finish trial proposal preparation: %w", err)
	}
	if !running {
		return ProposalOutput{}, admin.ErrTrialNotRunning
	}
	args, err := json.Marshal(trialExtensionChange{Days: input.Days})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode trial extension: %w", err)
	}
	expected, err := json.Marshal(state)
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode expected trial state: %w", err)
	}
	// Calendar days in UTC, matching the ExtendTrial query.
	preview, err := json.Marshal(trialPreview{OrganizationID: state.OrganizationID, Name: state.Name, Slug: state.Slug, Days: input.Days, EndsAt: state.Trial.EndsAt, NewEndsAt: trialTimestamp(endsAt.UTC().AddDate(0, 0, input.Days)), SideEffects: trialSideEffects})
	if err != nil {
		return ProposalOutput{}, fmt.Errorf("encode trial preview: %w", err)
	}
	proposal, replay, err := w.store.Create(ctx, owner, NewProposal{Operation: OperationExtendOrganizationTrial, SchemaVersion: 1, Target: ProposalTarget{OrganizationID: state.OrganizationID}, IdempotencyKey: input.RetryKey, Arguments: args, ExpectedState: expected, Preview: preview}, time.Now())
	if err != nil {
		return ProposalOutput{}, err
	}
	return proposalOutput(proposal, replay, w.baseURL), nil
}

// view renders the stored preview for the approval page. A preview that does
// not name the proposal's own organization is never shown.
func (w *trialWriter) view(proposal Proposal) (proposalView, error) {
	var preview trialPreview
	if err := json.Unmarshal(proposal.Preview, &preview); err != nil || preview.OrganizationID == "" || preview.OrganizationID != proposal.Target.OrganizationID || !validTrialExtensionDays(preview.Days) {
		return proposalView{}, ErrProposalInvalidated
	}
	endsAt, err := time.Parse(time.RFC3339Nano, preview.EndsAt)
	if err != nil {
		return proposalView{}, ErrProposalInvalidated
	}
	newEndsAt, err := time.Parse(time.RFC3339Nano, preview.NewEndsAt)
	if err != nil {
		return proposalView{}, ErrProposalInvalidated
	}
	unit := "days"
	if preview.Days == 1 {
		unit = "day"
	}
	const display = "2 Jan 2006 15:04 UTC"
	return proposalView{
		Summary:          fmt.Sprintf("Extend the enterprise trial by %d %s", preview.Days, unit),
		OrganizationID:   preview.OrganizationID,
		OrganizationName: preview.Name,
		OrganizationSlug: preview.Slug,
		Changes:          []proposalViewChange{{Setting: "Trial end date", Before: endsAt.UTC().Format(display), After: newEndsAt.UTC().Format(display)}},
		SideEffects:      preview.SideEffects,
	}, nil
}

// execution extends the trial through the same seam as the admin API. There
// is no cache to lock or refresh.
func (w *trialWriter) execution(authority writeAuthority) ProposalExecution {
	return ProposalExecution{
		Run: func(ctx context.Context, tx pgx.Tx, proposal Proposal) (string, json.RawMessage, error) {
			change, err := w.expectedState(ctx, tx, proposal)
			if err != nil {
				return "", nil, err
			}
			ctx, actor, name := staffMutation(ctx, authority)
			extension, err := admin.ExtendTrialTx(ctx, tx, w.audit, w.store.logger, proposal.Target.OrganizationID, change.Days, admin.TrialActor{Principal: actor, DisplayName: name})
			if errors.Is(err, admin.ErrTrialNotRunning) {
				return "", nil, ErrStaleState
			}
			if err != nil {
				return "", nil, fmt.Errorf("extend trial: %w", err)
			}
			result, err := json.Marshal(trialReceipt{PreviousEndsAt: trialTimestamp(extension.PreviousEndsAt), EndsAt: trialTimestamp(extension.EndsAt)})
			if err != nil {
				return "", nil, fmt.Errorf("encode trial receipt: %w", err)
			}
			return "succeeded", result, nil
		},
	}
}

func (w *trialWriter) registerPrepare(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "prepare_extend_organization_trial", Title: "Prepare Organization Trial Extension", Description: "Prepare an exact, single-organization extension of a running enterprise trial by 1 to 365 calendar days, added to the current end date. Cannot shorten, start, re-arm or convert a trial. Returns a server-stored before/after preview and a private staff approval URL. Does not make the change. Requires admin:write."}, func(ctx context.Context, _ *mcp.CallToolRequest, input PrepareTrialExtensionInput) (*mcp.CallToolResult, ProposalOutput, error) {
		out, err := w.prepare(ctx, input)
		return nil, out, err
	})
}
