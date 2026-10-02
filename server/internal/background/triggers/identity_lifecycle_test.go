package triggers_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/speakeasy-api/gram/server/internal/assistantidentity"
	identityrepo "github.com/speakeasy-api/gram/server/internal/assistantidentity/repo"
	"github.com/speakeasy-api/gram/server/internal/background/triggers"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func (f identityFixture) resolve(t *testing.T, assistantID, triggerID uuid.UUID) assistantidentity.Resolution {
	t.Helper()
	result, err := testIdentityService.Resolve(t.Context(), f.db, "org-trigger-test", f.projectID, assistantID, triggerID)
	require.NoError(t, err)
	return result
}

func TestRootIdentityCreateRetargetPauseDelete(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	ctx := t.Context()
	item, err := f.app.Create(ctx, f.createParams())
	require.NoError(t, err)
	first := f.resolve(t, f.assistantID, item.ID)
	require.Equal(t, assistantidentity.Active, first.State)
	require.NotNil(t, first.Identity)
	nextAssistant := f.createAssistant(t, true)
	update := triggers.UpdateParams{ID: item.ID, ProjectID: f.projectID, DefinitionSlug: item.DefinitionSlug, Name: item.Name, EnvironmentID: item.EnvironmentID, TargetKind: item.TargetKind, TargetRef: nextAssistant.String(), TargetDisplay: item.TargetDisplay, Config: map[string]any{}, Status: item.Status}
	_, err = f.app.Update(ctx, update)
	require.NoError(t, err)
	next := f.resolve(t, nextAssistant, item.ID)
	require.Equal(t, assistantidentity.Active, next.State)
	require.Equal(t, first.Identity.Subject, next.Identity.Subject)
	require.Greater(t, next.Identity.TriggerGeneration, first.Identity.TriggerGeneration)
	require.Error(t, testIdentityService.Validate(ctx, f.db, *first.Identity))
	_, err = f.app.SetStatus(ctx, f.projectID, item.ID, triggers.StatusPaused)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Unavailable, f.resolve(t, nextAssistant, item.ID).State)
	require.ErrorIs(t, testIdentityService.Validate(ctx, f.db, *next.Identity), assistantidentity.ErrInvalidIdentity)
	_, err = f.app.SetStatus(ctx, f.projectID, item.ID, triggers.StatusActive)
	require.NoError(t, err)
	resumed := f.resolve(t, nextAssistant, item.ID)
	require.Equal(t, assistantidentity.Active, resumed.State)
	require.Equal(t, next.Identity, resumed.Identity)
	require.NoError(t, testIdentityService.Validate(ctx, f.db, *next.Identity))
	require.Equal(t, next.Identity.Subject, resumed.Identity.Subject)
	require.NoError(t, f.app.Delete(ctx, f.projectID, item.ID))
	require.Equal(t, assistantidentity.Tombstoned, f.resolve(t, nextAssistant, item.ID).State)
	require.ErrorIs(t, testIdentityService.Validate(ctx, f.db, *resumed.Identity), assistantidentity.ErrInvalidIdentity)
}

func TestRootIdentityRetargetLegacyDoesNotFallback(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	item, err := f.app.Create(t.Context(), f.createParams())
	require.NoError(t, err)
	original := f.resolve(t, f.assistantID, item.ID)
	legacyID := f.createAssistant(t, false)
	_, err = f.app.Update(t.Context(), triggers.UpdateParams{ID: item.ID, ProjectID: f.projectID, DefinitionSlug: item.DefinitionSlug, Name: item.Name, EnvironmentID: item.EnvironmentID, TargetKind: item.TargetKind, TargetRef: legacyID.String(), TargetDisplay: item.TargetDisplay, Config: map[string]any{}, Status: item.Status})
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Tombstoned, f.resolve(t, legacyID, item.ID).State)
	require.ErrorIs(t, testIdentityService.Validate(t.Context(), f.db, *original.Identity), assistantidentity.ErrInvalidIdentity)
}

func TestRootIdentityConcurrentResumeIsIdempotent(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	item, err := f.app.Create(t.Context(), f.createParams())
	require.NoError(t, err)
	before := f.resolve(t, f.assistantID, item.ID)
	_, err = f.app.SetStatus(t.Context(), f.projectID, item.ID, triggers.StatusPaused)
	require.NoError(t, err)
	var group errgroup.Group
	for range 4 {
		group.Go(func() error {
			_, err := f.app.SetStatus(t.Context(), f.projectID, item.ID, triggers.StatusActive)
			if err != nil {
				return fmt.Errorf("resume trigger concurrently: %w", err)
			}
			return nil
		})
	}
	require.NoError(t, group.Wait())
	after := f.resolve(t, f.assistantID, item.ID)
	require.Equal(t, assistantidentity.Active, after.State)
	require.Equal(t, before.Identity.TriggerGeneration, after.Identity.TriggerGeneration)
	require.Equal(t, before.Identity.Subject, after.Identity.Subject)
}

func TestRootIdentityCreateCompensationPreservesTombstone(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	params := f.createParams()
	params.DefinitionSlug = triggers.DefinitionSlugCron
	params.Config = map[string]any{"schedule": "0 * * * *"}
	var triggerID uuid.UUID
	_, err := f.app.Create(t.Context(), params, func(_ context.Context, _ pgx.Tx, item triggerrepo.TriggerInstance) error {
		triggerID = item.ID
		return nil
	})
	require.Error(t, err, "no Temporal client forces post-commit schedule compensation")
	require.NotEqual(t, uuid.Nil, triggerID)
	require.Equal(t, assistantidentity.Tombstoned, f.resolve(t, f.assistantID, triggerID).State)
	_, err = f.app.GetInstance(t.Context(), f.projectID, triggerID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestContinuationCreateNeverMintsRootIdentity(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	root, err := f.app.Create(t.Context(), f.createParams())
	require.NoError(t, err)
	before := f.resolve(t, f.assistantID, root.ID)
	var wakeID uuid.UUID
	_, err = f.app.CreateWakeInstance(t.Context(), triggers.CreateWakeInstanceParams{OrganizationID: "org-trigger-test", ProjectID: f.projectID, Name: "Follow up", AssistantID: f.assistantID, TargetDisplay: "Assistant", FireAt: time.Now().Add(time.Hour), Note: nil, CorrelationID: "thread-identity"}, func(ctx context.Context, tx pgx.Tx, item triggerrepo.TriggerInstance) error {
		wakeID = item.ID
		_, err := identityrepo.New(tx).GetTriggerBinding(ctx, identityrepo.GetTriggerBindingParams{PlatformIssuer: "https://platform.example.invalid", PlatformJwksUri: "https://platform.example.invalid/.well-known/jwks.json", OrganizationID: "org-trigger-test", ProjectID: f.projectID, TriggerID: item.ID})
		if !errors.Is(err, pgx.ErrNoRows) {
			return errors.New("wake unexpectedly acquired a root binding")
		}
		return nil
	})
	require.Error(t, err, "no Temporal client forces workflow compensation")
	require.NotEqual(t, uuid.Nil, wakeID)
	_, err = identityrepo.New(f.db).GetTriggerBinding(t.Context(), identityrepo.GetTriggerBindingParams{PlatformIssuer: "https://platform.example.invalid", PlatformJwksUri: "https://platform.example.invalid/.well-known/jwks.json", OrganizationID: "org-trigger-test", ProjectID: f.projectID, TriggerID: wakeID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	after := f.resolve(t, f.assistantID, root.ID)
	require.Equal(t, before, after)
}

func TestRootIdentityPausedCreateAndMutationRollback(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	params := f.createParams()
	params.Status = triggers.StatusPaused
	item, err := f.app.Create(t.Context(), params)
	require.NoError(t, err)
	_, err = identityrepo.New(f.db).GetTriggerBinding(t.Context(), identityrepo.GetTriggerBindingParams{PlatformIssuer: "https://platform.example.invalid", PlatformJwksUri: "https://platform.example.invalid/.well-known/jwks.json", OrganizationID: "org-trigger-test", ProjectID: f.projectID, TriggerID: item.ID})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.app.SetStatus(t.Context(), f.projectID, item.ID, triggers.StatusActive)
	require.NoError(t, err)
	before := f.resolve(t, f.assistantID, item.ID)
	hookErr := errors.New("reject mutation")
	_, err = f.app.SetStatus(t.Context(), f.projectID, item.ID, triggers.StatusPaused, func(context.Context, pgx.Tx, triggerrepo.TriggerInstance) error { return hookErr })
	require.ErrorIs(t, err, hookErr)
	require.Equal(t, before, f.resolve(t, f.assistantID, item.ID))
	err = f.app.Delete(t.Context(), f.projectID, item.ID, func(context.Context, pgx.Tx, triggerrepo.TriggerInstance) error { return hookErr })
	require.ErrorIs(t, err, hookErr)
	require.Equal(t, before, f.resolve(t, f.assistantID, item.ID))
	// The one-shot workflow completion helper cannot fire a root trigger.
	require.NoError(t, f.app.MarkInstanceFired(t.Context(), item.ID.String()))
	persisted, err := f.app.GetInstance(t.Context(), f.projectID, item.ID)
	require.NoError(t, err)
	require.Equal(t, triggers.StatusActive, persisted.Status)
	require.Equal(t, before, f.resolve(t, f.assistantID, item.ID))
}

func TestRootIdentityUpdatePausePreservesIdentityAndPausedRetargetReplacesIt(t *testing.T) {
	t.Parallel()
	f := newIdentityFixture(t)
	ctx := t.Context()
	item, err := f.app.Create(ctx, f.createParams())
	require.NoError(t, err)
	first := f.resolve(t, f.assistantID, item.ID)
	update := triggers.UpdateParams{ID: item.ID, ProjectID: f.projectID, DefinitionSlug: item.DefinitionSlug, Name: item.Name, EnvironmentID: item.EnvironmentID, TargetKind: item.TargetKind, TargetRef: item.TargetRef, TargetDisplay: item.TargetDisplay, Config: map[string]any{}, Status: triggers.StatusPaused}
	_, err = f.app.Update(ctx, update)
	require.NoError(t, err)
	require.Equal(t, assistantidentity.Unavailable, f.resolve(t, f.assistantID, item.ID).State)
	_, err = f.app.SetStatus(ctx, f.projectID, item.ID, triggers.StatusActive)
	require.NoError(t, err)
	require.Equal(t, first.Identity, f.resolve(t, f.assistantID, item.ID).Identity)
	nextAssistant := f.createAssistant(t, true)
	update.TargetRef = nextAssistant.String()
	_, err = f.app.Update(ctx, update)
	require.NoError(t, err)
	require.ErrorIs(t, testIdentityService.Validate(ctx, f.db, *first.Identity), assistantidentity.ErrInvalidIdentity)
	_, err = f.app.SetStatus(ctx, f.projectID, item.ID, triggers.StatusActive)
	require.NoError(t, err)
	next := f.resolve(t, nextAssistant, item.ID)
	require.Equal(t, assistantidentity.Active, next.State)
	require.Greater(t, next.Identity.TriggerGeneration, first.Identity.TriggerGeneration)
}
