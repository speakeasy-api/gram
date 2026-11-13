package assistants

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// KitSwitchOutcome is what a staff switch to the Kit runtime did to one
// assistant.
type KitSwitchOutcome string

const (
	// KitSwitchSwitched means the assistant now selects Kit and its previous
	// runtimes are retired.
	KitSwitchSwitched KitSwitchOutcome = "switched"
	// KitSwitchAlreadyKit means the assistant already selected Kit.
	KitSwitchAlreadyKit KitSwitchOutcome = "already_kit"
	// KitSwitchNeedsIdentity means Kit could not boot the assistant because it
	// has no usable dedicated agent.
	KitSwitchNeedsIdentity KitSwitchOutcome = "needs_identity"
	// KitSwitchIneligible means Kit could not serve the assistant because its
	// project has no shared user-session issuer to mint runtime tokens from.
	KitSwitchIneligible KitSwitchOutcome = "ineligible"
	// KitSwitchBusy means the assistant has work in flight or a runtime that
	// may still be finishing a turn. Running again later retries it.
	KitSwitchBusy KitSwitchOutcome = "busy"
)

// StaffSwitchToKit selects the Kit runtime for one assistant inside tx without
// contacting any runtime backend. It switches only an assistant whose previous
// runtimes cannot be mid-turn: each live runtime is stopped, failed, or active
// past its warm window by more than the grace its thread workflow has to expire
// it. The switch marks those runtimes switch_retiring, and the
// runtime janitor stops their resources. The caller commits tx and audits a
// KitSwitchSwitched outcome.
func StaffSwitchToKit(ctx context.Context, tx pgx.Tx, organizationID string, projectID, assistantID uuid.UUID, now time.Time) (KitSwitchOutcome, error) {
	queries := assistantrepo.New(tx)
	if err := queries.AcquireAssistantAdvisoryLock(ctx, assistantID.String()); err != nil {
		return "", fmt.Errorf("lock assistant runtime selection: %w", err)
	}
	assistant, err := queries.GetAssistant(ctx, assistantrepo.GetAssistantParams{ProjectID: projectID, AssistantID: assistantID})
	if err != nil {
		return "", fmt.Errorf("read assistant runtime selection: %w", err)
	}
	if assistant.OrganizationID != organizationID {
		return "", fmt.Errorf("assistant belongs to another organization")
	}
	switch assistant.RuntimeVersion {
	case 3:
		return KitSwitchAlreadyKit, nil
	case 2:
	default:
		return "", fmt.Errorf("assistant has unknown runtime version %d", assistant.RuntimeVersion)
	}

	if err := validateRuntimeReasoningEffort(3, reasoningEffortFromPGText(assistant.ReasoningEffort)); err != nil {
		return "", err
	}

	// Kit bootstrap resolves this identity and refuses to start without it.
	if _, err := assistantidentity.ResolveRuntimeLocked(ctx, tx, organizationID, projectID, assistantID); errors.Is(err, assistantidentity.ErrInvalidIdentity) {
		return KitSwitchNeedsIdentity, nil
	} else if err != nil {
		return "", fmt.Errorf("resolve assistant runtime identity: %w", err)
	}
	// Kit composition falls back to any shared issuer of the project when no
	// attached MCP server names one, and fails only when there is none.
	issuers, err := usersessionsrepo.New(tx).ListSharedUserSessionIssuersInOrganization(ctx, usersessionsrepo.ListSharedUserSessionIssuersInOrganizationParams{OrganizationID: organizationID, ProjectID: uuid.NullUUID{UUID: projectID, Valid: true}})
	if err != nil {
		return "", fmt.Errorf("find Kit runtime issuer: %w", err)
	}
	if len(issuers) == 0 {
		return KitSwitchIneligible, nil
	}

	busy, err := queries.AssistantHasUnsettledEvents(ctx, assistantrepo.AssistantHasUnsettledEventsParams{ProjectID: projectID, AssistantID: assistantID})
	if err != nil {
		return "", fmt.Errorf("check pending assistant work: %w", err)
	}
	if !busy.Valid || busy.Bool {
		return KitSwitchBusy, nil
	}
	pending, err := queries.HasPendingRuntimeSwitchRetirement(ctx, assistantrepo.HasPendingRuntimeSwitchRetirementParams{ProjectID: projectID, AssistantID: assistantID})
	if err != nil {
		return "", fmt.Errorf("check pending runtime retirement: %w", err)
	}
	if pending {
		return KitSwitchBusy, nil
	}
	runtimes, err := queries.ListAssistantRuntimesForSwitch(ctx, assistantrepo.ListAssistantRuntimesForSwitchParams{ProjectID: projectID, AssistantID: assistantID})
	if err != nil {
		return "", fmt.Errorf("load runtimes for switching: %w", err)
	}
	for _, runtime := range runtimes {
		// Without a backend to ask, only a warm window that expired before the
		// idle sweep grace shows that an active legacy runtime has finished
		// turn-end persistence and compaction and is not being expired.
		idle := runtime.State == runtimeStateStopped || runtime.State == runtimeStateFailed ||
			(runtime.State == runtimeStateActive && runtime.WarmUntil.Valid && runtime.WarmUntil.Time.Before(now.Add(-runtimeIdleSweepGrace)))
		if runtime.RuntimeVersion == 3 || !idle {
			return KitSwitchBusy, nil
		}
	}

	if err := queries.RetireAssistantRuntimesForSwitch(ctx, assistantrepo.RetireAssistantRuntimesForSwitchParams{ProjectID: projectID, AssistantID: assistantID}); err != nil {
		return "", fmt.Errorf("retire runtime bindings: %w", err)
	}
	updated, err := queries.SetAssistantDesiredRuntimeVersion(ctx, assistantrepo.SetAssistantDesiredRuntimeVersionParams{ProjectID: projectID, AssistantID: assistantID, RuntimeVersion: 3, ExpectedRuntimeVersion: 2})
	if err != nil {
		return "", fmt.Errorf("persist runtime selection: %w", err)
	}
	if updated != 1 {
		return "", fmt.Errorf("assistant runtime selection changed during the switch")
	}
	return KitSwitchSwitched, nil
}
