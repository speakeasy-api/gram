//nolint:glint,paralleltest,tparallel // Integration tests intentionally inspect and corrupt private rows; subtests share one database.
package killswitches

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestLifecycleVersionsSnapshotsAndStaleReferences(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_lifecycle")
	var principalValid atomic.Bool
	principalValid.Store(true)
	var resourceValid atomic.Bool
	resourceValid.Store(true)
	var validatedMu sync.Mutex
	var validated []ResourceKey
	service := newLifecycleServiceForTest(t, conn, func(_ OrganizationID, _ PrincipalKey) bool {
		return principalValid.Load()
	}, func(_ OrganizationID, key ResourceKey) bool {
		validatedMu.Lock()
		validated = append(validated, key)
		validatedMu.Unlock()
		return resourceValid.Load()
	}, nil)

	startsAt := time.Date(2027, 1, 2, 3, 4, 5, 0, time.FixedZone("offset", 2*60*60))
	expiresAt := startsAt.Add(48 * time.Hour)
	activate := testActivateRequest(orgID, uuid.New())
	activate.Desired = DesiredVersionInput{ResourceScope: ResourceScopeSelected, SelectedResourceInputs: []string{" Tool:B ", "tool:a", "TOOL:B"}, StartMode: StartModeAt, StartsAt: &startsAt, ExpiresAt: &expiresAt, InternalNote: "  required context  ", ExternalNote: "  Access paused.  "}
	activated, err := service.ActivatePrescription(t.Context(), activate)
	require.NoError(t, err)
	require.Equal(t, int64(1), activated.Version)

	prescription, err := getPrescriptionForTest(t.Context(), conn, OrganizationID(orgID), activated.PrescriptionID)
	require.NoError(t, err)
	require.Equal(t, int64(1), prescription.CurrentVersion)
	v1 := requireVersion(t, prescription, 1)
	require.Equal(t, []ResourceKey{ResourceKey(orgID + ":tool:a"), ResourceKey(orgID + ":tool:b")}, v1.SelectedResourceKeys)
	require.Equal(t, "required context", v1.InternalNote)
	require.Equal(t, "Access paused.", v1.ExternalNote)
	require.Equal(t, StartModeAt, v1.StartMode)
	require.Equal(t, startsAt.UTC(), v1.StartsAt)

	validatedMu.Lock()
	validated = nil
	validatedMu.Unlock()
	changeNote := testChangeRequest(orgID, activated.PrescriptionID, 1, uuid.New(), []string{"tool:b", "tool:a"})
	changeNote.Desired.StartMode = StartModeAt
	changeNote.Desired.StartsAt = &startsAt
	changeNote.Desired.ExpiresAt = &expiresAt
	changeNote.Desired.InternalNote = "note only"
	changed, err := service.ChangePrescription(t.Context(), changeNote)
	require.NoError(t, err)
	require.Equal(t, int64(2), changed.Version)
	validatedMu.Lock()
	require.Empty(t, validated, "unchanged stale resources must not be revalidated")
	validatedMu.Unlock()

	validatedMu.Lock()
	validated = nil
	validatedMu.Unlock()
	changeResources := testChangeRequest(orgID, activated.PrescriptionID, 2, uuid.New(), []string{"tool:a", "tool:b", "tool:c"})
	changeResources.Desired.StartMode = StartModeAt
	changeResources.Desired.StartsAt = &startsAt
	changeResources.Desired.ExpiresAt = &expiresAt
	changeResources.Desired.InternalNote = "resource change"
	changed, err = service.ChangePrescription(t.Context(), changeResources)
	require.NoError(t, err)
	require.Equal(t, int64(3), changed.Version)
	validatedMu.Lock()
	require.Equal(t, []ResourceKey{ResourceKey(orgID + ":tool:a"), ResourceKey(orgID + ":tool:b"), ResourceKey(orgID + ":tool:c")}, validated)
	validatedMu.Unlock()

	principalValid.Store(false)
	resourceValid.Store(false)
	deactivate := DeactivatePrescriptionRequest{MutationContext: testMutationContext(orgID, uuid.New()), PrescriptionID: activated.PrescriptionID, ExpectedVersion: 3}
	deactivated, err := service.DeactivatePrescription(t.Context(), deactivate)
	require.NoError(t, err)
	require.Equal(t, PrescriptionStateInactive, deactivated.State)

	_, err = getPrescriptionForTest(t.Context(), conn, OrganizationID(orgID), activated.PrescriptionID)
	require.NoError(t, err, "reads must not consult stale source domains")

	reactivate := ReactivatePrescriptionRequest{MutationContext: testMutationContext(orgID, uuid.New()), PrescriptionID: activated.PrescriptionID, ExpectedVersion: 4, Desired: testDesired([]string{"tool:a"})}
	_, err = service.ReactivatePrescription(t.Context(), reactivate)
	require.ErrorIs(t, err, ErrInvalidReference)
	principalValid.Store(true)
	resourceValid.Store(true)
	reactivated, err := service.ReactivatePrescription(t.Context(), reactivate)
	require.NoError(t, err, "failed mutation must not consume its operation ID")
	require.Equal(t, int64(5), reactivated.Version)

	principalValid.Store(false)
	resourceValid.Store(false)
	for _, replay := range []struct {
		name    string
		version int64
		mutate  func() (MutationResult, error)
	}{
		{name: "change note", version: 2, mutate: func() (MutationResult, error) { return service.ChangePrescription(t.Context(), changeNote) }},
		{name: "change resources", version: 3, mutate: func() (MutationResult, error) { return service.ChangePrescription(t.Context(), changeResources) }},
		{name: "deactivate", version: 4, mutate: func() (MutationResult, error) { return service.DeactivatePrescription(t.Context(), deactivate) }},
		{name: "reactivate", version: 5, mutate: func() (MutationResult, error) { return service.ReactivatePrescription(t.Context(), reactivate) }},
	} {
		t.Run("replay "+replay.name, func(t *testing.T) {
			result, err := replay.mutate()
			require.NoError(t, err)
			require.True(t, result.Replayed)
			require.Equal(t, replay.version, result.Version)
		})
	}

	prescription, err = getPrescriptionForTest(t.Context(), conn, OrganizationID(orgID), activated.PrescriptionID)
	require.NoError(t, err)
	require.Len(t, prescription.Versions, 5)
	require.NotNil(t, requireVersion(t, prescription, 1).SupersededAt)
	require.Equal(t, []ResourceKey{ResourceKey(orgID + ":tool:a"), ResourceKey(orgID + ":tool:b")}, requireVersion(t, prescription, 2).SelectedResourceKeys)
	require.Equal(t, []ResourceKey{ResourceKey(orgID + ":tool:a"), ResourceKey(orgID + ":tool:b"), ResourceKey(orgID + ":tool:c")}, requireVersion(t, prescription, 4).SelectedResourceKeys)
	require.Equal(t, PrescriptionStateInactive, requireVersion(t, prescription, 4).State)
	require.True(t, requireVersion(t, prescription, 5).ActivatedAt.After(*requireVersion(t, prescription, 4).ActivatedAt) || requireVersion(t, prescription, 5).ActivatedAt.Equal(*requireVersion(t, prescription, 4).ActivatedAt))
}

func TestLifecycleReplayConflictAndBoundedReceipt(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_replay")
	var referencesValid atomic.Bool
	referencesValid.Store(true)
	service := newLifecycleServiceForTest(t, conn, func(OrganizationID, PrincipalKey) bool { return referencesValid.Load() }, func(OrganizationID, ResourceKey) bool { return referencesValid.Load() }, nil)

	operationID := uuid.New()
	startUTC := time.Date(2027, 2, 3, 4, 5, 6, 100, time.UTC)
	expiryUTC := startUTC.Add(24 * time.Hour)
	first := testActivateRequest(orgID, operationID)
	first.Desired = DesiredVersionInput{ResourceScope: ResourceScopeSelected, SelectedResourceInputs: []string{"tool:b", "tool:a", "tool:b"}, StartMode: StartModeAt, StartsAt: &startUTC, ExpiresAt: &expiryUTC, InternalNote: " replay note ", ExternalNote: " paused "}
	result, err := service.ActivatePrescription(t.Context(), first)
	require.NoError(t, err)
	require.False(t, result.Replayed)

	referencesValid.Store(false)
	offset := time.FixedZone("equivalent", -5*60*60)
	startOffset := startUTC.Add(700 * time.Nanosecond).In(offset)
	expiryOffset := expiryUTC.Add(700 * time.Nanosecond).In(offset)
	retry := first
	retry.PrincipalInput = " USER:ALPHA "
	retry.Desired = DesiredVersionInput{ResourceScope: ResourceScopeSelected, SelectedResourceInputs: []string{" TOOL:A ", "tool:b"}, StartMode: StartModeAt, StartsAt: &startOffset, ExpiresAt: &expiryOffset, InternalNote: "replay note", ExternalNote: "paused"}
	replayed, err := service.ActivatePrescription(t.Context(), retry)
	require.NoError(t, err)
	require.True(t, replayed.Replayed)
	require.Equal(t, result.PrescriptionID, replayed.PrescriptionID)

	conflict := retry
	conflict.Desired.InternalNote = "different"
	_, err = service.ActivatePrescription(t.Context(), conflict)
	require.ErrorIs(t, err, ErrOperationConflict)
	_, err = service.DeactivatePrescription(t.Context(), DeactivatePrescriptionRequest{MutationContext: testMutationContext(orgID, operationID), PrescriptionID: result.PrescriptionID, ExpectedVersion: 1})
	require.ErrorIs(t, err, ErrOperationConflict, "operation type is part of the receipt identity")

	var prescriptions, operations int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM killswitch_prescriptions WHERE organization_id = $1`, orgID).Scan(&prescriptions))
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM killswitch_operations WHERE organization_id = $1`, orgID).Scan(&operations))
	require.Equal(t, 1, prescriptions)
	require.Equal(t, 1, operations)
	var exactRetention, boundedResponse bool
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT expires_at = created_at + interval '30 days',
		       response ?& ARRAY['response_version', 'prescription_id', 'prescription_version', 'state']
		         AND (SELECT count(*) FROM jsonb_object_keys(response)) = 4
		FROM killswitch_operations
		WHERE organization_id = $1 AND operation_id = $2
	`, orgID, operationID).Scan(&exactRetention, &boundedResponse))
	require.True(t, exactRetention)
	require.True(t, boundedResponse)
}

func TestLifecycleConcurrentCreationAndCAS(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_concurrency")
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	var hookCalls atomic.Int64
	hook := func(_ context.Context, _ LifecycleTransactionQueries, event MutationEvent) error {
		if event.Operation == MutationOperationActivate && hookCalls.Add(1) == 1 {
			close(firstEntered)
			<-releaseFirst
		}
		return nil
	}
	service := newLifecycleServiceForTest(t, conn, nil, nil, hook)
	request := testActivateRequest(orgID, uuid.New())
	type outcome struct {
		result MutationResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	go func() {
		result, err := service.ActivatePrescription(t.Context(), request)
		outcomes <- outcome{result: result, err: err}
	}()
	<-firstEntered
	go func() {
		result, err := service.ActivatePrescription(t.Context(), request)
		outcomes <- outcome{result: result, err: err}
	}()
	select {
	case result := <-outcomes:
		close(releaseFirst)
		t.Fatalf("concurrent operation did not wait: %+v", result)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseFirst)
	first, second := <-outcomes, <-outcomes
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	replayCount := 0
	var prescriptionID PrescriptionID
	for _, result := range []MutationResult{first.result, second.result} {
		if result.Replayed {
			replayCount++
		}
		if prescriptionID == "" {
			prescriptionID = result.PrescriptionID
		}
		require.Equal(t, prescriptionID, result.PrescriptionID)
	}
	require.Equal(t, 1, replayCount)

	var wait sync.WaitGroup

	changeA := testChangeRequest(orgID, prescriptionID, 1, uuid.New(), []string{"tool:a"})
	changeA.Desired.InternalNote = "change a"
	changeB := changeA
	changeB.OperationID = uuid.New()
	changeB.Desired.InternalNote = "change b"
	changeErrs := make(chan error, 2)
	for _, change := range []ChangePrescriptionRequest{changeA, changeB} {
		wait.Add(1)
		go func(request ChangePrescriptionRequest) {
			defer wait.Done()
			_, err := service.ChangePrescription(t.Context(), request)
			changeErrs <- err
		}(change)
	}
	wait.Wait()
	close(changeErrs)
	successes, conflicts := 0, 0
	for err := range changeErrs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrVersionConflict):
			conflicts++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	prescription, err := getPrescriptionForTest(t.Context(), conn, OrganizationID(orgID), prescriptionID)
	require.NoError(t, err)
	require.Equal(t, int64(2), prescription.CurrentVersion)
	require.Len(t, prescription.Versions, 2)
}

func TestLifecycleAuthoritativeValidationUsesMutationTransaction(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_validation_tx")
	_, err := conn.Exec(t.Context(), `
		CREATE TABLE killswitch_validation_sources (
		  organization_id text NOT NULL,
		  reference_kind text NOT NULL,
		  reference_key text NOT NULL,
		  PRIMARY KEY (organization_id, reference_kind, reference_key)
		)
	`)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `
		INSERT INTO killswitch_validation_sources (organization_id, reference_kind, reference_key)
		VALUES ($1, 'principal', $2), ($1, 'resource', $3)
	`, orgID, "user:alpha", orgID+":tool:a")
	require.NoError(t, err)

	registry, err := BuildRegistry(validRegistration())
	require.NoError(t, err)
	beforeCommit := make(chan struct{})
	releaseCommit := make(chan struct{})
	hook := func(_ context.Context, queries LifecycleTransactionQueries, _ MutationEvent) error {
		if _, canCommit := queries.(interface{ Commit(context.Context) error }); canCommit {
			return errors.New("before-commit queries expose transaction completion")
		}
		close(beforeCommit)
		<-releaseCommit
		return nil
	}
	service, err := NewLifecycleService(conn, registry, lockingLifecycleValidator{}, hook)
	require.NoError(t, err)
	type mutationOutcome struct {
		result MutationResult
		err    error
	}
	mutationDone := make(chan mutationOutcome, 1)
	go func() {
		result, err := service.ActivatePrescription(t.Context(), testActivateRequest(orgID, uuid.New()))
		mutationDone <- mutationOutcome{result: result, err: err}
	}()
	select {
	case <-beforeCommit:
	case outcome := <-mutationDone:
		t.Fatalf("lifecycle mutation failed before commit barrier: %v", outcome.err)
	}

	deleteDone := make(chan error, 1)
	go func() {
		_, err := conn.Exec(t.Context(), `
			DELETE FROM killswitch_validation_sources
			WHERE organization_id = $1 AND reference_kind = 'resource' AND reference_key = $2
		`, orgID, orgID+":tool:a")
		deleteDone <- err
	}()
	select {
	case err := <-deleteDone:
		close(releaseCommit)
		t.Fatalf("authoritative resource delete did not wait for lifecycle commit: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseCommit)
	outcome := <-mutationDone
	require.NoError(t, outcome.err)
	require.NotEmpty(t, outcome.result.PrescriptionID)
	require.NoError(t, <-deleteDone)
}

func TestLifecycleOrganizationIsolationAndCrossProjectResources(t *testing.T) {
	t.Parallel()

	conn, orgA := newLifecycleDatabase(t, "killswitch_tenancy")
	orgB := "org_" + uuid.NewString()
	insertOrganization(t, conn, orgB)
	service := newLifecycleServiceForTest(t, conn, nil, func(org OrganizationID, key ResourceKey) bool {
		return string(org) == orgA && (key == ResourceKey(orgA+":project:a:tool") || key == ResourceKey(orgA+":project:b:tool"))
	}, nil)

	request := testActivateRequest(orgA, uuid.New())
	request.Desired.SelectedResourceInputs = []string{"project:a:tool", "project:b:tool"}
	result, err := service.ActivatePrescription(t.Context(), request)
	require.NoError(t, err)
	prescription, err := getPrescriptionForTest(t.Context(), conn, OrganizationID(orgA), result.PrescriptionID)
	require.NoError(t, err)
	require.Len(t, requireVersion(t, prescription, 1).SelectedResourceKeys, 2)

	_, err = getPrescriptionForTest(t.Context(), conn, OrganizationID(orgB), result.PrescriptionID)
	require.ErrorIs(t, err, ErrPrescriptionNotFound)
	_, err = service.DeactivatePrescription(t.Context(), DeactivatePrescriptionRequest{MutationContext: testMutationContext(orgB, uuid.New()), PrescriptionID: result.PrescriptionID, ExpectedVersion: 1})
	require.ErrorIs(t, err, ErrPrescriptionNotFound)
	_, err = service.ChangePrescription(t.Context(), testChangeRequest(orgB, result.PrescriptionID, 1, uuid.New(), []string{"project:a:tool"}))
	require.ErrorIs(t, err, ErrPrescriptionNotFound)
	crossOrganizationReplay := request
	crossOrganizationReplay.OrganizationID = OrganizationID(orgB)
	_, err = service.ActivatePrescription(t.Context(), crossOrganizationReplay)
	require.ErrorIs(t, err, ErrInvalidReference)

	var countA, countB int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM killswitch_prescriptions WHERE organization_id = $1`, orgA).Scan(&countA))
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM killswitch_prescriptions WHERE organization_id = $1`, orgB).Scan(&countB))
	require.Equal(t, 1, countA)
	require.Zero(t, countB)
}

func TestLifecycleScopeChangesValidateCurrentSelections(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_scope_changes")
	var resourcesValid atomic.Bool
	resourcesValid.Store(true)
	service := newLifecycleServiceForTest(t, conn, nil, func(OrganizationID, ResourceKey) bool { return resourcesValid.Load() }, nil)

	activated, err := service.ActivatePrescription(t.Context(), testActivateRequest(orgID, uuid.New()))
	require.NoError(t, err)
	resourcesValid.Store(false)
	toAll := testChangeRequest(orgID, activated.PrescriptionID, 1, uuid.New(), nil)
	toAll.Desired.ResourceScope = ResourceScopeAll
	changed, err := service.ChangePrescription(t.Context(), toAll)
	require.NoError(t, err, "selected to all has no new resource reference to validate")

	toSelected := testChangeRequest(orgID, activated.PrescriptionID, changed.Version, uuid.New(), []string{"tool:b"})
	_, err = service.ChangePrescription(t.Context(), toSelected)
	require.ErrorIs(t, err, ErrInvalidReference)
	resourcesValid.Store(true)
	changed, err = service.ChangePrescription(t.Context(), toSelected)
	require.NoError(t, err, "failed validation must not consume the operation ID")

	prescription, err := getPrescriptionForTest(t.Context(), conn, OrganizationID(orgID), activated.PrescriptionID)
	require.NoError(t, err)
	require.Equal(t, ResourceScopeAll, requireVersion(t, prescription, 2).ResourceScope)
	require.Empty(t, requireVersion(t, prescription, 2).SelectedResourceKeys)
	require.Equal(t, []ResourceKey{ResourceKey(orgID + ":tool:b")}, requireVersion(t, prescription, 3).SelectedResourceKeys)
}

func TestLifecycleDynamicAllIntervalsRollbackCleanupAndReclaim(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_boundaries")
	var resourceValidations atomic.Int64
	hookErr := errors.New("injected before-commit failure")
	rollbackService := newLifecycleServiceForTest(t, conn, nil, func(OrganizationID, ResourceKey) bool {
		resourceValidations.Add(1)
		return true
	}, func(_ context.Context, _ LifecycleTransactionQueries, _ MutationEvent) error { return hookErr })

	operationID := uuid.New()
	request := testActivateRequest(orgID, operationID)
	request.Desired = DesiredVersionInput{ResourceScope: ResourceScopeAll, StartMode: StartModeNow, InternalNote: "all resources", ExternalNote: "paused"}
	_, err := rollbackService.ActivatePrescription(t.Context(), request)
	require.ErrorIs(t, err, hookErr)
	require.Equal(t, int64(0), resourceValidations.Load())
	for _, table := range []string{"killswitch_prescriptions", "killswitch_prescription_versions", "killswitch_prescription_version_resources", "killswitch_operations"} {
		var count int
		require.NoError(t, conn.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" WHERE organization_id = $1", orgID).Scan(&count))
		require.Zero(t, count, table)
	}

	service := newLifecycleServiceForTest(t, conn, nil, func(OrganizationID, ResourceKey) bool {
		resourceValidations.Add(1)
		return true
	}, nil)
	first, err := service.ActivatePrescription(t.Context(), request)
	require.NoError(t, err, "rolled-back operation ID must remain reusable")
	prescription, err := getPrescriptionForTest(t.Context(), conn, OrganizationID(orgID), first.PrescriptionID)
	require.NoError(t, err)
	v1 := requireVersion(t, prescription, 1)
	require.Equal(t, ResourceScopeAll, v1.ResourceScope)
	require.Empty(t, v1.SelectedResourceKeys)
	require.Nil(t, v1.ExpiresAt)

	_, err = conn.Exec(t.Context(), `UPDATE killswitch_operations SET expires_at = clock_timestamp() - interval '1 second' WHERE organization_id = $1 AND operation_id = $2`, orgID, operationID)
	require.NoError(t, err)
	reusedOperation := request
	reusedOperation.Desired.InternalNote = "different request after receipt expiry"
	second, err := service.ActivatePrescription(t.Context(), reusedOperation)
	require.NoError(t, err, "expired operation IDs may be reclaimed by a different request")
	require.False(t, second.Replayed)
	require.NotEqual(t, first.PrescriptionID, second.PrescriptionID)

	otherOrgID := "org_" + uuid.NewString()
	insertOrganization(t, conn, otherOrgID)
	sharedOperationID := uuid.New()
	expiredOperations := []struct {
		organizationID string
		operationID    uuid.UUID
	}{
		{organizationID: orgID, operationID: sharedOperationID},
		{organizationID: orgID, operationID: uuid.New()},
		{organizationID: orgID, operationID: uuid.New()},
		{organizationID: otherOrgID, operationID: sharedOperationID},
		{organizationID: otherOrgID, operationID: uuid.New()},
		{organizationID: otherOrgID, operationID: uuid.New()},
	}
	for _, expired := range expiredOperations {
		_, err = conn.Exec(t.Context(), `
			INSERT INTO killswitch_operations (organization_id, operation_id, actor_user_id, operation, request_hash, expires_at)
			VALUES ($1, $2, 'user:test', 'change', 'sha256:test', clock_timestamp() - interval '1 second')
		`, expired.organizationID, expired.operationID)
		require.NoError(t, err)
	}
	deleted, err := service.CleanupExpiredOperations(t.Context(), OrganizationID(orgID), 2)
	require.NoError(t, err)
	require.Equal(t, int64(2), deleted)
	deleted, err = service.CleanupExpiredOperations(t.Context(), OrganizationID(orgID), 2)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	var otherOrgOperations int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM killswitch_operations WHERE organization_id = $1`, otherOrgID).Scan(&otherOrgOperations))
	require.Equal(t, 3, otherOrgOperations, "organization cleanup must not sweep another organization")
	_, err = service.CleanupExpiredOperations(t.Context(), OrganizationID(orgID), 0)
	require.ErrorIs(t, err, ErrInvalidArgument)
	_, err = service.CleanupExpiredOperations(t.Context(), "", 1)
	require.ErrorIs(t, err, ErrInvalidArgument)
}

func TestLifecycleCollaboratorsCannotCompleteTransaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		validator LifecycleValidator
		hook      BeforeCommitHook
	}{
		{
			name: "validator commit through exec",
			validator: lifecycleValidatorFunc(func(ctx context.Context, queries LifecycleTransactionQueries, _ CurrentReferenceBatch) error {
				_, err := queries.Exec(ctx, " /* leading comment */ COMMIT")
				return fmt.Errorf("validator commit: %w", err)
			}),
		},
		{
			name: "validator commit through query rewriter",
			validator: lifecycleValidatorFunc(func(ctx context.Context, queries LifecycleTransactionQueries, _ CurrentReferenceBatch) error {
				_, err := queries.Exec(ctx, "SELECT 1", rewritingLifecycleQuery{sql: "COMMIT"})
				return fmt.Errorf("validator query rewriter commit: %w", err)
			}),
		},
		{
			name: "validator rollback through query",
			validator: lifecycleValidatorFunc(func(ctx context.Context, queries LifecycleTransactionQueries, _ CurrentReferenceBatch) error {
				rows, err := queries.Query(ctx, "-- leading comment\nROLLBACK")
				if err != nil {
					return fmt.Errorf("validator rollback: %w", err)
				}
				rows.Close()
				return errors.New("transaction control unexpectedly allowed")
			}),
		},
		{
			name:      "hook commit through query row",
			validator: fakeLifecycleValidator{},
			hook: func(ctx context.Context, queries LifecycleTransactionQueries, _ MutationEvent) error {
				return fmt.Errorf("hook commit: %w", queries.QueryRow(ctx, "/* leading comment */ COMMIT").Scan())
			},
		},
		{
			name:      "hook multi-statement after non-ASCII identifier",
			validator: fakeLifecycleValidator{},
			hook: func(ctx context.Context, queries LifecycleTransactionQueries, _ MutationEvent) error {
				_, err := queries.Exec(ctx, "SELECT 1 AS fooα$tag$; COMMIT -- $tag$")
				return fmt.Errorf("hook multiple statements: %w", err)
			},
		},
		{
			name:      "hook rollback through exec",
			validator: fakeLifecycleValidator{},
			hook: func(ctx context.Context, queries LifecycleTransactionQueries, _ MutationEvent) error {
				_, err := queries.Exec(ctx, "\n ROLLBACK")
				return fmt.Errorf("hook rollback: %w", err)
			},
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn, orgID := newLifecycleDatabase(t, fmt.Sprintf("killswitch_transaction_control_%d", i))
			registry, err := BuildRegistry(validRegistration())
			require.NoError(t, err)
			service, err := NewLifecycleService(conn, registry, test.validator, test.hook)
			require.NoError(t, err)

			_, err = service.ActivatePrescription(t.Context(), testActivateRequest(orgID, uuid.New()))
			require.ErrorIs(t, err, errLifecycleTransactionQueryRejected)
			for _, table := range []string{"killswitch_prescriptions", "killswitch_prescription_versions", "killswitch_prescription_version_resources", "killswitch_operations"} {
				var count int
				require.NoError(t, conn.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" WHERE organization_id = $1", orgID).Scan(&count))
				require.Zero(t, count, table+" must not be partially durable")
			}
		})
	}
}

func TestLifecycleSuccessorRollbackRestoresCurrentVersion(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_successor_rollback")
	service := newLifecycleServiceForTest(t, conn, nil, nil, nil)
	activated, err := service.ActivatePrescription(t.Context(), testActivateRequest(orgID, uuid.New()))
	require.NoError(t, err)

	hookErr := errors.New("injected successor before-commit failure")
	failingService := newLifecycleServiceForTest(t, conn, nil, nil, func(_ context.Context, _ LifecycleTransactionQueries, _ MutationEvent) error { return hookErr })
	operationID := uuid.New()
	_, err = failingService.ChangePrescription(t.Context(), testChangeRequest(orgID, activated.PrescriptionID, 1, operationID, []string{"tool:b"}))
	require.ErrorIs(t, err, hookErr)

	prescription, err := getPrescriptionForTest(t.Context(), conn, OrganizationID(orgID), activated.PrescriptionID)
	require.NoError(t, err)
	require.Equal(t, int64(1), prescription.CurrentVersion)
	require.Len(t, prescription.Versions, 1)
	v1 := requireVersion(t, prescription, 1)
	require.Nil(t, v1.SupersededAt)
	require.Equal(t, []ResourceKey{ResourceKey(orgID + ":tool:a")}, v1.SelectedResourceKeys)

	var operationCount int
	require.NoError(t, conn.QueryRow(t.Context(), `
		SELECT count(*)
		FROM killswitch_operations
		WHERE organization_id = $1 AND operation_id = $2
	`, orgID, operationID).Scan(&operationCount))
	require.Zero(t, operationCount)
}

func TestLifecycleRejectsInvalidTransitionsAndReceiptPayload(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_invalid")
	service := newLifecycleServiceForTest(t, conn, nil, nil, nil)
	request := testActivateRequest(orgID, uuid.New())
	result, err := service.ActivatePrescription(t.Context(), request)
	require.NoError(t, err)

	_, err = service.ReactivatePrescription(t.Context(), ReactivatePrescriptionRequest{MutationContext: testMutationContext(orgID, uuid.New()), PrescriptionID: result.PrescriptionID, ExpectedVersion: 1, Desired: testDesired([]string{"tool:a"})})
	require.ErrorIs(t, err, ErrInvalidTransition)
	deactivated, err := service.DeactivatePrescription(t.Context(), DeactivatePrescriptionRequest{MutationContext: testMutationContext(orgID, uuid.New()), PrescriptionID: result.PrescriptionID, ExpectedVersion: 1})
	require.NoError(t, err)
	_, err = service.ChangePrescription(t.Context(), testChangeRequest(orgID, result.PrescriptionID, deactivated.Version, uuid.New(), []string{"tool:a"}))
	require.ErrorIs(t, err, ErrInvalidTransition)

	badOperation := uuid.New()
	badRequest := testActivateRequest(orgID, badOperation)
	_, err = service.ActivatePrescription(t.Context(), badRequest)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `
		UPDATE killswitch_operations
		SET status = 'pending', response = NULL
		WHERE organization_id = $1 AND operation_id = $2
	`, orgID, badOperation)
	require.NoError(t, err)
	_, err = service.ActivatePrescription(t.Context(), badRequest)
	require.ErrorIs(t, err, ErrOperationUnavailable)

	_, err = conn.Exec(t.Context(), `
		UPDATE killswitch_operations
		SET status = 'completed',
		    response = jsonb_build_object('response_version', 'future', 'prescription_id', $3::text, 'prescription_version', 1, 'state', 'active')
		WHERE organization_id = $1 AND operation_id = $2
	`, orgID, badOperation, result.PrescriptionID)
	require.NoError(t, err)
	_, err = service.ActivatePrescription(t.Context(), badRequest)
	require.ErrorIs(t, err, ErrOperationUnavailable)
}

func newLifecycleDatabase(t *testing.T, name string) (*pgxpool.Pool, string) {
	t.Helper()
	conn, err := infra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	orgID := "org_" + uuid.NewString()
	insertOrganization(t, conn, orgID)
	return conn, orgID
}

type lockingLifecycleValidator struct{}

func (lockingLifecycleValidator) ValidateCurrent(ctx context.Context, queries LifecycleTransactionQueries, batch CurrentReferenceBatch) error {
	if batch.Principal != nil {
		var key string
		err := queries.QueryRow(ctx, `
			SELECT reference_key
			FROM killswitch_validation_sources
			WHERE organization_id = $1 AND reference_kind = 'principal' AND reference_key = $2
			FOR KEY SHARE
		`, batch.OrganizationID, batch.Principal.Key).Scan(&key)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: principal is not current in the organization", ErrInvalidReference)
		}
		if err != nil {
			return fmt.Errorf("lock current principal reference: %w", err)
		}
	}
	if batch.Resources == nil {
		return nil
	}
	keys := make([]string, len(batch.Resources.Keys))
	for i, key := range batch.Resources.Keys {
		keys[i] = string(key)
	}
	rows, err := queries.Query(ctx, `
		SELECT reference_key
		FROM killswitch_validation_sources
		WHERE organization_id = $1 AND reference_kind = 'resource' AND reference_key = ANY($2::text[])
		ORDER BY reference_key
		FOR KEY SHARE
	`, batch.OrganizationID, keys)
	if err != nil {
		return fmt.Errorf("lock current resource references: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return fmt.Errorf("scan current resource reference: %w", err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate current resource references: %w", err)
	}
	if count != len(keys) {
		return fmt.Errorf("%w: resources are not current in the organization", ErrInvalidReference)
	}
	return nil
}

type lifecycleValidatorFunc func(context.Context, LifecycleTransactionQueries, CurrentReferenceBatch) error

func (f lifecycleValidatorFunc) ValidateCurrent(ctx context.Context, queries LifecycleTransactionQueries, batch CurrentReferenceBatch) error {
	return f(ctx, queries, batch)
}

type fakeLifecycleValidator struct {
	principal func(OrganizationID, PrincipalKey) bool
	resource  func(OrganizationID, ResourceKey) bool
}

func (v fakeLifecycleValidator) ValidateCurrent(_ context.Context, _ LifecycleTransactionQueries, batch CurrentReferenceBatch) error {
	if batch.Principal != nil && v.principal != nil && !v.principal(batch.OrganizationID, batch.Principal.Key) {
		return fmt.Errorf("%w: principal is not current in the organization", ErrInvalidReference)
	}
	if batch.Resources == nil || v.resource == nil {
		return nil
	}
	for _, key := range batch.Resources.Keys {
		if !v.resource(batch.OrganizationID, key) {
			return fmt.Errorf("%w: resource %q is not current in the organization", ErrInvalidReference, key)
		}
	}
	return nil
}

func newLifecycleServiceForTest(t *testing.T, conn *pgxpool.Pool, principalValidation func(OrganizationID, PrincipalKey) bool, resourceValidation func(OrganizationID, ResourceKey) bool, hook BeforeCommitHook) *LifecycleService {
	t.Helper()
	registry, err := BuildRegistry(validRegistration())
	require.NoError(t, err)
	service, err := NewLifecycleService(conn, registry, fakeLifecycleValidator{principal: principalValidation, resource: resourceValidation}, hook)
	require.NoError(t, err)
	return service
}

func testMutationContext(orgID string, operationID uuid.UUID) MutationContext {
	return MutationContext{OrganizationID: OrganizationID(orgID), ActorUserID: "user:test", ActorDisplayName: "Test User", OperationID: operationID}
}

func testActivateRequest(orgID string, operationID uuid.UUID) ActivatePrescriptionRequest {
	return ActivatePrescriptionRequest{MutationContext: testMutationContext(orgID, operationID), Definition: "block-tools", PrincipalKind: "user", PrincipalInput: "User:Alpha", ResourceKind: "tool", Desired: testDesired([]string{"tool:a"})}
}

func testChangeRequest(orgID string, prescriptionID PrescriptionID, expectedVersion int64, operationID uuid.UUID, resources []string) ChangePrescriptionRequest {
	return ChangePrescriptionRequest{MutationContext: testMutationContext(orgID, operationID), PrescriptionID: prescriptionID, ExpectedVersion: expectedVersion, Desired: testDesired(resources)}
}

func testDesired(resources []string) DesiredVersionInput {
	return DesiredVersionInput{ResourceScope: ResourceScopeSelected, SelectedResourceInputs: resources, StartMode: StartModeNow, InternalNote: "required context", ExternalNote: "Access paused."}
}

type testPrescription struct {
	CurrentVersion int64
	Versions       []testPrescriptionVersion
}

type testPrescriptionVersion struct {
	Version              int64
	State                PrescriptionState
	ResourceScope        ResourceScope
	StartMode            StartMode
	SelectedResourceKeys []ResourceKey
	StartsAt             time.Time
	ExpiresAt            *time.Time
	ActivatedAt          *time.Time
	SupersededAt         *time.Time
	InternalNote         string
	ExternalNote         string
}

func getPrescriptionForTest(ctx context.Context, conn *pgxpool.Pool, organizationID OrganizationID, prescriptionID PrescriptionID) (testPrescription, error) {
	id, err := uuid.Parse(string(prescriptionID))
	if err != nil {
		return testPrescription{}, fmt.Errorf("parse test prescription ID: %w", err)
	}
	var result testPrescription
	err = conn.QueryRow(ctx, `
		SELECT current_version
		FROM killswitch_prescriptions
		WHERE organization_id = $1 AND id = $2
	`, organizationID, id).Scan(&result.CurrentVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return testPrescription{}, ErrPrescriptionNotFound
	}
	if err != nil {
		return testPrescription{}, fmt.Errorf("get test prescription: %w", err)
	}
	rows, err := conn.Query(ctx, `
		SELECT
		  version.version,
		  version.state,
		  version.resource_scope,
		  version.start_mode,
		  version.starts_at,
		  version.expires_at,
		  version.activated_at,
		  version.superseded_at,
		  version.internal_note,
		  version.external_note,
		  ARRAY(
		    SELECT resource.resource_key
		    FROM killswitch_prescription_version_resources AS resource
		    WHERE resource.organization_id = version.organization_id
		      AND resource.prescription_id = version.prescription_id
		      AND resource.version = version.version
		    ORDER BY resource.resource_key
		  )::text[]
		FROM killswitch_prescription_versions AS version
		WHERE version.organization_id = $1 AND version.prescription_id = $2
		ORDER BY version.version
	`, organizationID, id)
	if err != nil {
		return testPrescription{}, fmt.Errorf("list test prescription versions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var version testPrescriptionVersion
		var state, scope string
		var resources []string
		if err := rows.Scan(&version.Version, &state, &scope, &version.StartMode, &version.StartsAt, &version.ExpiresAt, &version.ActivatedAt, &version.SupersededAt, &version.InternalNote, &version.ExternalNote, &resources); err != nil {
			return testPrescription{}, fmt.Errorf("scan test prescription version: %w", err)
		}
		version.State = PrescriptionState(state)
		version.ResourceScope = ResourceScope(scope)
		version.StartsAt = version.StartsAt.UTC()
		for _, value := range []*time.Time{version.ExpiresAt, version.ActivatedAt, version.SupersededAt} {
			if value != nil {
				*value = value.UTC()
			}
		}
		version.SelectedResourceKeys = make([]ResourceKey, len(resources))
		for i, resource := range resources {
			version.SelectedResourceKeys[i] = ResourceKey(resource)
		}
		result.Versions = append(result.Versions, version)
	}
	if err := rows.Err(); err != nil {
		return testPrescription{}, fmt.Errorf("iterate test prescription versions: %w", err)
	}
	return result, nil
}

func requireVersion(t *testing.T, prescription testPrescription, version int64) testPrescriptionVersion {
	t.Helper()
	for _, candidate := range prescription.Versions {
		if candidate.Version == version {
			return candidate
		}
	}
	t.Fatalf("version %d not found in %v", version, prescription.Versions)
	return testPrescriptionVersion{}
}

func TestLifecycleInputValidation(t *testing.T) {
	t.Parallel()

	conn, orgID := newLifecycleDatabase(t, "killswitch_input")
	service := newLifecycleServiceForTest(t, conn, nil, nil, nil)

	tests := []struct {
		name    string
		wantErr error
		mutate  func(*ActivatePrescriptionRequest)
	}{
		{name: "nil operation ID", wantErr: ErrInvalidArgument, mutate: func(r *ActivatePrescriptionRequest) { r.OperationID = uuid.Nil }},
		{name: "unknown definition", wantErr: ErrInvalidReference, mutate: func(r *ActivatePrescriptionRequest) { r.Definition = "unknown" }},
		{name: "empty selected set", wantErr: ErrInvalidArgument, mutate: func(r *ActivatePrescriptionRequest) { r.Desired.SelectedResourceInputs = nil }},
		{name: "all with selected keys", wantErr: ErrInvalidArgument, mutate: func(r *ActivatePrescriptionRequest) { r.Desired.ResourceScope = ResourceScopeAll }},
		{name: "empty internal note", wantErr: ErrInvalidArgument, mutate: func(r *ActivatePrescriptionRequest) { r.Desired.InternalNote = "  " }},
		{name: "raw selected resource limit", wantErr: ErrInvalidArgument, mutate: func(r *ActivatePrescriptionRequest) {
			r.Desired.SelectedResourceInputs = make([]string, maxSelectedResourceCount+1)
			for i := range r.Desired.SelectedResourceInputs {
				r.Desired.SelectedResourceInputs[i] = "tool:a"
			}
		}},
		{name: "expiry before now start", wantErr: ErrInvalidArgument, mutate: func(r *ActivatePrescriptionRequest) { past := time.Now().Add(-time.Hour); r.Desired.ExpiresAt = &past }},
		{name: "interval collapses at postgres precision", wantErr: ErrInvalidArgument, mutate: func(r *ActivatePrescriptionRequest) {
			startsAt := time.Date(2027, 1, 2, 3, 4, 5, 100, time.UTC)
			expiresAt := startsAt.Add(time.Nanosecond)
			r.Desired.StartMode = StartModeAt
			r.Desired.StartsAt = &startsAt
			r.Desired.ExpiresAt = &expiresAt
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := testActivateRequest(orgID, uuid.New())
			test.mutate(&request)
			_, err := service.ActivatePrescription(t.Context(), request)
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}
