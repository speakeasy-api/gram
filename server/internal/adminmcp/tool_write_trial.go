//nolint:exhaustruct // MCP SDK manifests intentionally use documented optional defaults.
package adminmcp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/speakeasy-api/gram/server/internal/admin"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/constants"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	trialsrepo "github.com/speakeasy-api/gram/server/internal/trials/repo"
)

const trialSideEffects = "Extends the enterprise trial end date and records a tenant audit event. No billing, model key, account type or feature change."

// TrialExtensionInput is one exact extension of a trial end date.
type TrialExtensionInput struct {
	OrganizationID string `json:"organization_id" jsonschema:"Exact canonical organization ID from find_organizations, not a slug"`
	Days           int    `json:"days" jsonschema:"Calendar days to add to the current trial end date, 1 to 365"`
	ExpectedEndsAt string `json:"expected_ends_at" jsonschema:"Current trial end date as RFC3339, copied from ends_at in get_organization_trial. Refused if the end date has changed since, so repeating a call cannot extend twice"`
}

type trialState struct {
	OrganizationID string
	Name           string
	Slug           string
	EndsAt         time.Time
	Running        bool
}

type trialReceipt struct {
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Days           int    `json:"days"`
	PreviousEndsAt string `json:"previous_ends_at"`
	EndsAt         string `json:"ends_at"`
	SideEffects    string `json:"side_effects"`
}

func validTrialExtensionDays(days int) bool {
	return days >= constants.MinTrialExtensionDays && days <= constants.MaxTrialExtensionDays
}

func trialTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// sameTrialEnd compares at the second precision get_organization_trial reports.
func sameTrialEnd(a, b time.Time) bool {
	return a.Truncate(time.Second).Equal(b.Truncate(time.Second))
}

// trialWriter extends running trials. Extension adds to ends_at, so it is not
// idempotent by itself: checking expected_ends_at under the trial row lock is
// what stops a repeated call extending again.
type trialWriter struct {
	runner *writeRunner
	audit  *audit.Logger
	writes WriteConfig
}

// readState locks the trial row that every trial lifecycle change locks. It
// returns admin.ErrTrialNotRunning when the organization has no trial row.
func (w *trialWriter) readState(ctx context.Context, tx pgx.Tx, organizationID string) (trialState, error) {
	org, err := orgrepo.New(tx).GetOrganizationMetadata(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && org.ID != organizationID) {
		return trialState{}, ErrStaleState
	}
	if err != nil {
		return trialState{}, fmt.Errorf("read trial target: %w", err)
	}
	trial, err := trialsrepo.New(tx).LockTrialLifecycle(ctx, organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return trialState{}, admin.ErrTrialNotRunning
	}
	if err != nil {
		return trialState{}, fmt.Errorf("lock trial lifecycle: %w", err)
	}
	running := !trial.ConvertedAt.Valid && !trial.DemotedAt.Valid && trial.EndsAt.Time.After(time.Now())
	return trialState{OrganizationID: org.ID, Name: org.Name, Slug: org.Slug, EndsAt: trial.EndsAt.Time, Running: running}, nil
}

// apply extends the trial through the same seam as the admin API. There is no
// cache to lock or refresh.
func (w *trialWriter) apply(ctx context.Context, input TrialExtensionInput) (trialReceipt, error) {
	authority, err := requireWriteAuthority(ctx, w.writes, OperationExtendOrganizationTrial)
	if err != nil {
		return trialReceipt{}, err
	}
	if err := checkWriteTarget(input.OrganizationID); err != nil {
		return trialReceipt{}, err
	}
	// Checked on the wide int, as admin.ExtendTrial does.
	if !validTrialExtensionDays(input.Days) {
		return trialReceipt{}, fmt.Errorf("days must be between %d and %d", constants.MinTrialExtensionDays, constants.MaxTrialExtensionDays)
	}
	expectedEndsAt, err := time.Parse(time.RFC3339Nano, input.ExpectedEndsAt)
	if err != nil {
		return trialReceipt{}, errors.New("provide expected_ends_at as the RFC3339 ends_at from get_organization_trial")
	}
	return runWrite(ctx, w.runner, authority, writeTarget{OrganizationID: input.OrganizationID}, func(ctx context.Context, tx pgx.Tx) (trialReceipt, error) {
		state, err := w.readState(ctx, tx, input.OrganizationID)
		if err != nil {
			return trialReceipt{}, err
		}
		if !state.Running {
			return trialReceipt{}, admin.ErrTrialNotRunning
		}
		if !sameTrialEnd(state.EndsAt, expectedEndsAt) {
			return trialReceipt{}, ErrStaleState
		}
		ctx, actor, name := staffMutation(ctx, authority)
		extension, err := admin.ExtendTrialTx(ctx, tx, w.audit, w.runner.logger, input.OrganizationID, input.Days, admin.TrialActor{Principal: actor, DisplayName: name})
		if err != nil {
			return trialReceipt{}, fmt.Errorf("extend trial: %w", err)
		}
		return trialReceipt{OrganizationID: state.OrganizationID, Name: state.Name, Slug: state.Slug, Days: input.Days, PreviousEndsAt: trialTimestamp(extension.PreviousEndsAt), EndsAt: trialTimestamp(extension.EndsAt), SideEffects: trialSideEffects}, nil
	})
}

func (w *trialWriter) register(server *mcp.Server) {
	addWriteTool(server, &mcp.Tool{Name: string(OperationExtendOrganizationTrial), Title: "Extend Organization Trial", Description: "Make an exact, single-organization extension of a running enterprise trial by 1 to 365 calendar days, added to the current end date. Read the current end date with get_organization_trial first. Cannot shorten, start, re-arm or convert a trial."}, w.apply)
}
