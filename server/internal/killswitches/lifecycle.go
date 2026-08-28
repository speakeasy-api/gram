//nolint:exhaustruct // Canonical payloads and nullable database values intentionally use documented zero values.
package killswitches

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/killswitches/repo"
)

const (
	operationStatusPending   = "pending"
	operationStatusCompleted = "completed"
)

// LifecycleService owns killswitch mutation transactions and durable operation replay.
type LifecycleService struct {
	db           *pgxpool.Pool
	registry     *Registry
	validator    LifecycleValidator
	beforeCommit BeforeCommitHook
}

func NewLifecycleService(db *pgxpool.Pool, registry *Registry, validator LifecycleValidator, beforeCommit BeforeCommitHook) (*LifecycleService, error) {
	if db == nil || registry == nil || isNilInterface(validator) {
		return nil, ErrInvalidArgument
	}
	return &LifecycleService{db: db, registry: registry, validator: validator, beforeCommit: beforeCommit}, nil
}

type mutationQueries struct {
	repo       *repo.Queries
	restricted LifecycleTransactionQueries
}

type versionResourceSnapshot struct {
	keys            []ResourceKey
	copyFromVersion int64
}

type operationReceipt struct {
	actorUserID string
	operation   string
	requestHash string
	status      string
	response    []byte
}

type canonicalDesiredVersion struct {
	resourceScope ResourceScope
	resourceKeys  []ResourceKey
	startMode     StartMode
	startsAt      *time.Time
	expiresAt     *time.Time
	internalNote  string
	externalNote  string
}

type prescriptionIdentity struct {
	id            uuid.UUID
	definition    DefinitionKey
	principalKind PrincipalKind
	principalKey  PrincipalKey
	resourceKind  ResourceKind
}

type lockedCurrent struct {
	ID             uuid.UUID
	OrganizationID string
	DefinitionKey  string
	PrincipalKind  string
	PrincipalKey   string
	ResourceKind   string
	CurrentVersion int64
	State          string
	ResourceScope  string
	StartsAt       pgtype.Timestamptz
	ExpiresAt      pgtype.Timestamptz
	ActivatedAt    pgtype.Timestamptz
	SupersededAt   pgtype.Timestamptz
	InternalNote   string
	ExternalNote   string
}

func (s *LifecycleService) ActivatePrescription(ctx context.Context, request ActivatePrescriptionRequest) (MutationResult, error) {
	if err := validateMutationContext(request.MutationContext); err != nil {
		return MutationResult{}, err
	}
	definition, ok := s.registry.Definition(request.Definition)
	if !ok {
		return MutationResult{}, fmt.Errorf("%w: definition %q is not registered", ErrInvalidReference, request.Definition)
	}
	if !slices.Contains(definition.PrincipalKinds, request.PrincipalKind) || !slices.Contains(definition.ResourceKinds, request.ResourceKind) {
		return MutationResult{}, fmt.Errorf("%w: definition %q does not support principal %q and resource %q", ErrInvalidReference, request.Definition, request.PrincipalKind, request.ResourceKind)
	}
	principalAdapter, ok := s.registry.PrincipalAdapter(request.PrincipalKind)
	if !ok {
		return MutationResult{}, fmt.Errorf("%w: principal kind %q is not registered", ErrInvalidReference, request.PrincipalKind)
	}
	resourceAdapter, ok := s.registry.ResourceAdapter(request.ResourceKind)
	if !ok {
		return MutationResult{}, fmt.Errorf("%w: resource kind %q is not registered", ErrInvalidReference, request.ResourceKind)
	}
	principalKey, err := canonicalPrincipal(request.OrganizationID, principalAdapter, request.PrincipalInput)
	if err != nil {
		return MutationResult{}, err
	}
	desired, err := canonicalDesired(request.OrganizationID, resourceAdapter, request.Desired)
	if err != nil {
		return MutationResult{}, err
	}
	canonical := canonicalActivationMutation(request, principalKey, desired)
	requestHash, err := CanonicalMutationHashV1(canonical)
	if err != nil {
		return MutationResult{}, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}

	return s.executeMutation(ctx, request.MutationContext, MutationOperationActivate, requestHash, func(queries mutationQueries) (MutationResult, error) {
		if err := s.validateCurrent(ctx, queries.restricted, CurrentReferenceBatch{
			OrganizationID: request.OrganizationID,
			Principal:      &CurrentPrincipalReference{Kind: request.PrincipalKind, Key: principalKey},
			Resources:      currentResourceReferences(request.ResourceKind, desired.resourceKeys),
		}); err != nil {
			return MutationResult{}, err
		}
		transitionTime, err := databaseTime(ctx, queries.repo)
		if err != nil {
			return MutationResult{}, err
		}
		startsAt, err := desired.resolvedStartsAt(transitionTime)
		if err != nil {
			return MutationResult{}, err
		}

		headerID, err := queries.repo.CreateKillswitchPrescriptionHeader(ctx, repo.CreateKillswitchPrescriptionHeaderParams{
			OrganizationID: string(request.OrganizationID),
			DefinitionKey:  string(request.Definition),
			PrincipalKind:  string(request.PrincipalKind),
			PrincipalKey:   string(principalKey),
			ResourceKind:   string(request.ResourceKind),
		})
		if err != nil {
			return MutationResult{}, fmt.Errorf("create killswitch prescription header: %w", err)
		}
		if err := createVersion(ctx, queries.repo, repo.CreateKillswitchPrescriptionVersionParams{
			OrganizationID: string(request.OrganizationID), PrescriptionID: headerID, Version: 1,
			State: string(PrescriptionStateActive), ResourceScope: string(desired.resourceScope),
			StartsAt: conv.ToPGTimestamptz(startsAt), ExpiresAt: conv.PtrToPGTimestamptz(desired.expiresAt),
			ActivatedAt: conv.ToPGTimestamptz(transitionTime), InternalNote: desired.internalNote, ExternalNote: desired.externalNote,
		}, versionResourceSnapshot{keys: desired.resourceKeys}); err != nil {
			return MutationResult{}, err
		}
		return MutationResult{PrescriptionID: PrescriptionID(headerID.String()), Version: 1, State: PrescriptionStateActive}, nil
	})
}

func (s *LifecycleService) ChangePrescription(ctx context.Context, request ChangePrescriptionRequest) (MutationResult, error) {
	if err := validateExistingRequest(request.MutationContext, request.PrescriptionID, request.ExpectedVersion); err != nil {
		return MutationResult{}, err
	}
	identity, resourceAdapter, err := s.existingResourceAdapter(ctx, request.OrganizationID, request.PrescriptionID)
	if err != nil {
		return MutationResult{}, err
	}
	desired, err := canonicalDesired(request.OrganizationID, resourceAdapter, request.Desired)
	if err != nil {
		return MutationResult{}, err
	}
	canonical := canonicalExistingMutation(MutationOperationChange, request.PrescriptionID, request.ExpectedVersion, desired)
	requestHash, err := CanonicalMutationHashV1(canonical)
	if err != nil {
		return MutationResult{}, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}

	return s.executeMutation(ctx, request.MutationContext, MutationOperationChange, requestHash, func(queries mutationQueries) (MutationResult, error) {
		current, err := lockCurrent(ctx, queries.repo, request.OrganizationID, identity.id, request.ExpectedVersion)
		if err != nil {
			return MutationResult{}, err
		}
		if err := ensureIdentity(current, identity); err != nil {
			return MutationResult{}, err
		}
		if PrescriptionState(current.State) != PrescriptionStateActive {
			return MutationResult{}, fmt.Errorf("%w: change requires an active prescription", ErrInvalidTransition)
		}
		resources, err := changedSelectedResources(ctx, queries.repo, current, desired)
		if err != nil {
			return MutationResult{}, err
		}
		if len(resources) > 0 {
			if err := s.validateCurrent(ctx, queries.restricted, CurrentReferenceBatch{OrganizationID: request.OrganizationID, Resources: currentResourceReferences(identity.resourceKind, resources)}); err != nil {
				return MutationResult{}, err
			}
		}
		transitionTime, err := databaseTime(ctx, queries.repo)
		if err != nil {
			return MutationResult{}, err
		}
		startsAt, err := desired.resolvedStartsAt(transitionTime)
		if err != nil {
			return MutationResult{}, err
		}
		if !current.ActivatedAt.Valid {
			return MutationResult{}, errors.New("active killswitch version has no activation time")
		}
		return transitionExisting(ctx, queries.repo, current, desired.resourceScope, versionResourceSnapshot{keys: desired.resourceKeys}, PrescriptionStateActive, startsAt, desired.expiresAt, current.ActivatedAt.Time, desired.internalNote, desired.externalNote, transitionTime)
	})
}

func (s *LifecycleService) DeactivatePrescription(ctx context.Context, request DeactivatePrescriptionRequest) (MutationResult, error) {
	if err := validateExistingRequest(request.MutationContext, request.PrescriptionID, request.ExpectedVersion); err != nil {
		return MutationResult{}, err
	}
	prescriptionUUID, err := parsePrescriptionID(request.PrescriptionID)
	if err != nil {
		return MutationResult{}, err
	}
	expected := request.ExpectedVersion
	canonical := CanonicalMutationV1{Operation: MutationOperationDeactivate, PrescriptionID: &request.PrescriptionID, ExpectedVersion: &expected}
	requestHash, err := CanonicalMutationHashV1(canonical)
	if err != nil {
		return MutationResult{}, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}

	return s.executeMutation(ctx, request.MutationContext, MutationOperationDeactivate, requestHash, func(queries mutationQueries) (MutationResult, error) {
		current, err := lockCurrent(ctx, queries.repo, request.OrganizationID, prescriptionUUID, request.ExpectedVersion)
		if err != nil {
			return MutationResult{}, err
		}
		if PrescriptionState(current.State) != PrescriptionStateActive {
			return MutationResult{}, fmt.Errorf("%w: deactivate requires an active prescription", ErrInvalidTransition)
		}
		transitionTime, err := databaseTime(ctx, queries.repo)
		if err != nil {
			return MutationResult{}, err
		}
		activatedAt := optionalTime(current.ActivatedAt)
		if activatedAt == nil {
			return MutationResult{}, errors.New("active killswitch version has no activation time")
		}
		return transitionExisting(ctx, queries.repo, current, ResourceScope(current.ResourceScope), versionResourceSnapshot{copyFromVersion: current.CurrentVersion}, PrescriptionStateInactive, current.StartsAt.Time, optionalTime(current.ExpiresAt), *activatedAt, current.InternalNote, current.ExternalNote, transitionTime)
	})
}

func (s *LifecycleService) ReactivatePrescription(ctx context.Context, request ReactivatePrescriptionRequest) (MutationResult, error) {
	if err := validateExistingRequest(request.MutationContext, request.PrescriptionID, request.ExpectedVersion); err != nil {
		return MutationResult{}, err
	}
	identity, resourceAdapter, err := s.existingResourceAdapter(ctx, request.OrganizationID, request.PrescriptionID)
	if err != nil {
		return MutationResult{}, err
	}
	desired, err := canonicalDesired(request.OrganizationID, resourceAdapter, request.Desired)
	if err != nil {
		return MutationResult{}, err
	}
	canonical := canonicalExistingMutation(MutationOperationReactivate, request.PrescriptionID, request.ExpectedVersion, desired)
	requestHash, err := CanonicalMutationHashV1(canonical)
	if err != nil {
		return MutationResult{}, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}

	return s.executeMutation(ctx, request.MutationContext, MutationOperationReactivate, requestHash, func(queries mutationQueries) (MutationResult, error) {
		current, err := lockCurrent(ctx, queries.repo, request.OrganizationID, identity.id, request.ExpectedVersion)
		if err != nil {
			return MutationResult{}, err
		}
		if err := ensureIdentity(current, identity); err != nil {
			return MutationResult{}, err
		}
		if PrescriptionState(current.State) != PrescriptionStateInactive {
			return MutationResult{}, fmt.Errorf("%w: reactivate requires an inactive prescription", ErrInvalidTransition)
		}
		if err := s.validateCurrent(ctx, queries.restricted, CurrentReferenceBatch{
			OrganizationID: request.OrganizationID,
			Principal:      &CurrentPrincipalReference{Kind: identity.principalKind, Key: identity.principalKey},
			Resources:      currentResourceReferences(identity.resourceKind, desired.resourceKeys),
		}); err != nil {
			return MutationResult{}, err
		}
		transitionTime, err := databaseTime(ctx, queries.repo)
		if err != nil {
			return MutationResult{}, err
		}
		startsAt, err := desired.resolvedStartsAt(transitionTime)
		if err != nil {
			return MutationResult{}, err
		}
		return transitionExisting(ctx, queries.repo, current, desired.resourceScope, versionResourceSnapshot{keys: desired.resourceKeys}, PrescriptionStateActive, startsAt, desired.expiresAt, transitionTime, desired.internalNote, desired.externalNote, transitionTime)
	})
}

func (s *LifecycleService) executeMutation(ctx context.Context, mutation MutationContext, operation MutationOperation, requestHash string, apply func(mutationQueries) (MutationResult, error)) (MutationResult, error) {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return MutationResult{}, fmt.Errorf("begin killswitch lifecycle transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	restricted := lifecycleTransactionQueries{db: tx}
	queries := repo.New(restricted)

	receipt, fresh, err := claimOperation(ctx, queries, mutation, operation, requestHash)
	if err != nil {
		return MutationResult{}, err
	}
	if !fresh {
		return replayOperation(receipt, mutation.ActorUserID, operation, requestHash)
	}

	result, err := apply(mutationQueries{repo: queries, restricted: restricted})
	if err != nil {
		return MutationResult{}, err
	}
	response := operationResponseV1{ResponseVersion: operationResponseVersionV1, PrescriptionID: result.PrescriptionID, PrescriptionVersion: result.Version, State: result.State}
	encoded, err := json.Marshal(response)
	if err != nil {
		return MutationResult{}, fmt.Errorf("encode killswitch operation response: %w", err)
	}
	if s.beforeCommit != nil {
		event := MutationEvent{OrganizationID: mutation.OrganizationID, ActorUserID: mutation.ActorUserID, OperationID: mutation.OperationID, Operation: operation, Result: result}
		if err := s.beforeCommit(ctx, restricted, event); err != nil {
			return MutationResult{}, fmt.Errorf("before killswitch lifecycle commit: %w", err)
		}
	}
	rows, err := queries.CompleteKillswitchOperation(ctx, repo.CompleteKillswitchOperationParams{Response: encoded, OrganizationID: string(mutation.OrganizationID), OperationID: mutation.OperationID, Operation: string(operation), RequestHash: requestHash})
	if err != nil {
		return MutationResult{}, fmt.Errorf("complete killswitch operation: %w", err)
	}
	if rows != 1 {
		return MutationResult{}, errors.New("complete killswitch operation: expected one updated row")
	}
	if err := tx.Commit(ctx); err != nil {
		return MutationResult{}, fmt.Errorf("commit killswitch lifecycle transaction: %w", err)
	}
	return result, nil
}

func claimOperation(ctx context.Context, queries *repo.Queries, mutation MutationContext, operation MutationOperation, requestHash string) (operationReceipt, bool, error) {
	params := repo.ClaimKillswitchOperationParams{OrganizationID: string(mutation.OrganizationID), OperationID: mutation.OperationID, ActorUserID: mutation.ActorUserID, Operation: string(operation), RequestHash: requestHash}
	for range 3 {
		if _, err := queries.ClaimKillswitchOperation(ctx, params); err == nil {
			return operationReceipt{}, true, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return operationReceipt{}, false, fmt.Errorf("claim killswitch operation: %w", err)
		}
		locked, err := queries.LockKillswitchOperation(ctx, repo.LockKillswitchOperationParams{OrganizationID: string(mutation.OrganizationID), OperationID: mutation.OperationID})
		if err == nil {
			return operationReceipt{actorUserID: locked.ActorUserID, operation: locked.Operation, requestHash: locked.RequestHash, status: locked.Status, response: locked.Response}, false, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return operationReceipt{}, false, fmt.Errorf("lock killswitch operation: %w", err)
		}
	}
	return operationReceipt{}, false, fmt.Errorf("%w: operation receipt changed during reclaim", ErrOperationUnavailable)
}

func replayOperation(receipt operationReceipt, actorUserID string, operation MutationOperation, requestHash string) (MutationResult, error) {
	if receipt.actorUserID != actorUserID || receipt.operation != string(operation) || receipt.requestHash != requestHash {
		return MutationResult{}, ErrOperationConflict
	}
	if receipt.status != operationStatusCompleted || len(receipt.response) == 0 {
		return MutationResult{}, fmt.Errorf("%w: operation receipt is not completed", ErrOperationUnavailable)
	}
	var response operationResponseV1
	if err := json.Unmarshal(receipt.response, &response); err != nil {
		return MutationResult{}, fmt.Errorf("%w: decode operation response: %w", ErrOperationUnavailable, err)
	}
	if response.ResponseVersion != operationResponseVersionV1 || response.PrescriptionID == "" || response.PrescriptionVersion < 1 || !validPrescriptionState(response.State) {
		return MutationResult{}, fmt.Errorf("%w: unsupported operation response", ErrOperationUnavailable)
	}
	return MutationResult{PrescriptionID: response.PrescriptionID, Version: response.PrescriptionVersion, State: response.State, Replayed: true}, nil
}

func transitionExisting(ctx context.Context, queries *repo.Queries, current lockedCurrent, scope ResourceScope, snapshot versionResourceSnapshot, state PrescriptionState, startsAt time.Time, expiresAt *time.Time, activatedAt time.Time, internalNote, externalNote string, transitionTime time.Time) (MutationResult, error) {
	if current.SupersededAt.Valid {
		return MutationResult{}, errors.New("current killswitch version is already superseded")
	}
	rows, err := queries.SupersedeKillswitchPrescriptionVersion(ctx, repo.SupersedeKillswitchPrescriptionVersionParams{SupersededAt: conv.ToPGTimestamptz(transitionTime), OrganizationID: current.OrganizationID, PrescriptionID: current.ID, Version: current.CurrentVersion})
	if err != nil {
		return MutationResult{}, fmt.Errorf("supersede killswitch prescription version: %w", err)
	}
	if rows != 1 {
		return MutationResult{}, errors.New("supersede killswitch prescription version: expected one updated row")
	}
	newVersion := current.CurrentVersion + 1
	if err := createVersion(ctx, queries, repo.CreateKillswitchPrescriptionVersionParams{OrganizationID: current.OrganizationID, PrescriptionID: current.ID, Version: newVersion, State: string(state), ResourceScope: string(scope), StartsAt: conv.ToPGTimestamptz(startsAt), ExpiresAt: conv.PtrToPGTimestamptz(expiresAt), ActivatedAt: conv.ToPGTimestamptz(activatedAt), InternalNote: internalNote, ExternalNote: externalNote}, snapshot); err != nil {
		return MutationResult{}, err
	}
	rows, err = queries.AdvanceKillswitchPrescriptionCurrentVersion(ctx, repo.AdvanceKillswitchPrescriptionCurrentVersionParams{NewVersion: newVersion, UpdatedAt: conv.ToPGTimestamptz(transitionTime), OrganizationID: current.OrganizationID, PrescriptionID: current.ID, ExpectedVersion: current.CurrentVersion})
	if err != nil {
		return MutationResult{}, fmt.Errorf("advance killswitch prescription current version: %w", err)
	}
	if rows != 1 {
		return MutationResult{}, errors.New("killswitch current-version CAS failed after aggregate lock")
	}
	return MutationResult{PrescriptionID: PrescriptionID(current.ID.String()), Version: newVersion, State: state}, nil
}

func createVersion(ctx context.Context, queries *repo.Queries, params repo.CreateKillswitchPrescriptionVersionParams, snapshot versionResourceSnapshot) error {
	rows, err := queries.CreateKillswitchPrescriptionVersion(ctx, params)
	if err != nil {
		return fmt.Errorf("create killswitch prescription version: %w", err)
	}
	if rows != 1 {
		return errors.New("create killswitch prescription version: expected one inserted row")
	}
	if snapshot.copyFromVersion > 0 {
		rows, err = queries.CopyKillswitchPrescriptionVersionResources(ctx, repo.CopyKillswitchPrescriptionVersionResourcesParams{NewVersion: params.Version, OrganizationID: params.OrganizationID, PrescriptionID: params.PrescriptionID, SourceVersion: snapshot.copyFromVersion})
		if err != nil {
			return fmt.Errorf("copy killswitch prescription resource snapshot: %w", err)
		}
		return validateStoredResourceCount(ResourceScope(params.ResourceScope), rows)
	}
	keys := resourceKeysToStrings(snapshot.keys)
	rows, err = queries.CreateKillswitchPrescriptionVersionResources(ctx, repo.CreateKillswitchPrescriptionVersionResourcesParams{OrganizationID: params.OrganizationID, PrescriptionID: params.PrescriptionID, Version: params.Version, ResourceKeys: keys})
	if err != nil {
		return fmt.Errorf("create killswitch prescription resource snapshot: %w", err)
	}
	if rows != int64(len(keys)) {
		return fmt.Errorf("create killswitch prescription resource snapshot: inserted %d of %d rows", rows, len(keys))
	}
	return validateStoredResourceCount(ResourceScope(params.ResourceScope), rows)
}

func lockCurrent(ctx context.Context, queries *repo.Queries, organizationID OrganizationID, prescriptionID uuid.UUID, expectedVersion int64) (lockedCurrent, error) {
	header, err := queries.LockKillswitchPrescriptionCurrent(ctx, repo.LockKillswitchPrescriptionCurrentParams{OrganizationID: string(organizationID), PrescriptionID: prescriptionID})
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedCurrent{}, ErrPrescriptionNotFound
	}
	if err != nil {
		return lockedCurrent{}, fmt.Errorf("lock killswitch prescription: %w", err)
	}
	if header.CurrentVersion != expectedVersion {
		return lockedCurrent{}, &VersionConflictError{Expected: expectedVersion, Actual: header.CurrentVersion}
	}
	version, err := queries.GetKillswitchPrescriptionVersion(ctx, repo.GetKillswitchPrescriptionVersionParams{OrganizationID: string(organizationID), PrescriptionID: prescriptionID, Version: header.CurrentVersion})
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedCurrent{}, errors.New("killswitch current version does not exist")
	}
	if err != nil {
		return lockedCurrent{}, fmt.Errorf("get locked killswitch version: %w", err)
	}
	if err := validateStoredResourceScope(ResourceScope(version.ResourceScope)); err != nil {
		return lockedCurrent{}, err
	}
	current := lockedCurrent{ID: header.ID, OrganizationID: header.OrganizationID, DefinitionKey: header.DefinitionKey, PrincipalKind: header.PrincipalKind, PrincipalKey: header.PrincipalKey, ResourceKind: header.ResourceKind, CurrentVersion: header.CurrentVersion, State: version.State, ResourceScope: version.ResourceScope, StartsAt: version.StartsAt, ExpiresAt: version.ExpiresAt, ActivatedAt: version.ActivatedAt, SupersededAt: version.SupersededAt, InternalNote: version.InternalNote, ExternalNote: version.ExternalNote}
	return current, nil
}

func (s *LifecycleService) existingResourceAdapter(ctx context.Context, organizationID OrganizationID, prescriptionID PrescriptionID) (prescriptionIdentity, ResourceAdapter, error) {
	id, err := parsePrescriptionID(prescriptionID)
	if err != nil {
		return prescriptionIdentity{}, nil, err
	}
	row, err := repo.New(s.db).GetKillswitchPrescriptionIdentity(ctx, repo.GetKillswitchPrescriptionIdentityParams{OrganizationID: string(organizationID), PrescriptionID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return prescriptionIdentity{}, nil, ErrPrescriptionNotFound
	}
	if err != nil {
		return prescriptionIdentity{}, nil, fmt.Errorf("get killswitch prescription identity: %w", err)
	}
	identity := prescriptionIdentity{id: row.ID, definition: DefinitionKey(row.DefinitionKey), principalKind: PrincipalKind(row.PrincipalKind), principalKey: PrincipalKey(row.PrincipalKey), resourceKind: ResourceKind(row.ResourceKind)}
	adapter, ok := s.registry.ResourceAdapter(identity.resourceKind)
	if !ok {
		return prescriptionIdentity{}, nil, fmt.Errorf("%w: resource kind %q is not registered", ErrInvalidReference, identity.resourceKind)
	}
	return identity, adapter, nil
}

func canonicalDesired(organizationID OrganizationID, adapter ResourceAdapter, input DesiredVersionInput) (canonicalDesiredVersion, error) {
	if len(input.SelectedResourceInputs) > maxSelectedResourceCount {
		return canonicalDesiredVersion{}, fmt.Errorf("%w: selected resources exceed the limit of %d; use all-resource scope for broader coverage", ErrInvalidArgument, maxSelectedResourceCount)
	}
	externalNote, err := NormalizeExternalNote(input.ExternalNote)
	if err != nil {
		return canonicalDesiredVersion{}, fmt.Errorf("%w: external note: %w", ErrInvalidArgument, err)
	}
	internalNote, err := NormalizeInternalNote(input.InternalNote)
	if err != nil {
		return canonicalDesiredVersion{}, fmt.Errorf("%w: internal note: %w", ErrInvalidArgument, err)
	}
	keys := make([]ResourceKey, 0, len(input.SelectedResourceInputs))
	for _, resourceInput := range input.SelectedResourceInputs {
		result, err := adapter.Canonicalize(organizationID, resourceInput)
		if err != nil {
			return canonicalDesiredVersion{}, fmt.Errorf("canonicalize killswitch resource: %w", err)
		}
		key, supported, err := result.Key()
		if err != nil {
			return canonicalDesiredVersion{}, fmt.Errorf("canonicalize killswitch resource: %w", err)
		}
		if !supported {
			return canonicalDesiredVersion{}, fmt.Errorf("%w: unsupported resource reference", ErrInvalidReference)
		}
		keys = append(keys, key)
	}
	canonicalKeys, err := canonicalSelectedResources(&input.ResourceScope, keys)
	if err != nil {
		return canonicalDesiredVersion{}, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if len(canonicalKeys) > maxSelectedResourceCount {
		return canonicalDesiredVersion{}, fmt.Errorf("%w: canonical selected resources exceed the limit of %d; use all-resource scope for broader coverage", ErrInvalidArgument, maxSelectedResourceCount)
	}
	return canonicalDesiredVersion{resourceScope: input.ResourceScope, resourceKeys: canonicalKeys, startMode: input.StartMode, startsAt: clonePostgresTime(input.StartsAt), expiresAt: clonePostgresTime(input.ExpiresAt), internalNote: internalNote, externalNote: externalNote}, nil
}

func canonicalPrincipal(organizationID OrganizationID, adapter PrincipalAdapter, input string) (PrincipalKey, error) {
	result, err := adapter.Canonicalize(organizationID, input)
	if err != nil {
		return "", fmt.Errorf("canonicalize killswitch principal: %w", err)
	}
	key, supported, err := result.Key()
	if err != nil {
		return "", fmt.Errorf("canonicalize killswitch principal: %w", err)
	}
	if !supported {
		return "", fmt.Errorf("%w: unsupported principal reference", ErrInvalidReference)
	}
	return key, nil
}

func (s *LifecycleService) validateCurrent(ctx context.Context, queries LifecycleTransactionQueries, batch CurrentReferenceBatch) error {
	if batch.Principal == nil && batch.Resources == nil {
		return nil
	}
	if err := s.validator.ValidateCurrent(ctx, queries, batch); err != nil {
		return fmt.Errorf("validate current killswitch references: %w", err)
	}
	return nil
}

func currentResourceReferences(kind ResourceKind, keys []ResourceKey) *CurrentResourceReferences {
	if len(keys) == 0 {
		return nil
	}
	return &CurrentResourceReferences{Kind: kind, Keys: keys}
}

func canonicalActivationMutation(request ActivatePrescriptionRequest, principalKey PrincipalKey, desired canonicalDesiredVersion) CanonicalMutationV1 {
	definition, principalKind, resourceKind := request.Definition, request.PrincipalKind, request.ResourceKind
	canonical := canonicalDesiredMutationFields(desired)
	canonical.Operation = MutationOperationActivate
	canonical.Definition = &definition
	canonical.PrincipalKind = &principalKind
	canonical.PrincipalKey = &principalKey
	canonical.ResourceKind = &resourceKind
	return canonical
}

func canonicalExistingMutation(operation MutationOperation, prescriptionID PrescriptionID, expectedVersion int64, desired canonicalDesiredVersion) CanonicalMutationV1 {
	canonical := canonicalDesiredMutationFields(desired)
	canonical.Operation = operation
	canonical.PrescriptionID = &prescriptionID
	canonical.ExpectedVersion = &expectedVersion
	return canonical
}

func canonicalDesiredMutationFields(desired canonicalDesiredVersion) CanonicalMutationV1 {
	scope, mode := desired.resourceScope, desired.startMode
	externalNote, internalNote := desired.externalNote, desired.internalNote
	return CanonicalMutationV1{ResourceScope: &scope, SelectedResourceKeys: desired.resourceKeys, StartMode: &mode, StartsAt: clonePostgresTime(desired.startsAt), ExpiresAt: clonePostgresTime(desired.expiresAt), ExternalNote: &externalNote, InternalNote: &internalNote}
}

func (d canonicalDesiredVersion) resolvedStartsAt(databaseNow time.Time) (time.Time, error) {
	var startsAt time.Time
	switch d.startMode {
	case StartModeNow:
		startsAt = databaseNow
	case StartModeAt:
		if d.startsAt == nil {
			return time.Time{}, fmt.Errorf("%w: scheduled start requires a timestamp", ErrInvalidArgument)
		}
		startsAt = *d.startsAt
	default:
		return time.Time{}, fmt.Errorf("%w: invalid start mode %q", ErrInvalidArgument, d.startMode)
	}
	if d.expiresAt != nil && !startsAt.Before(*d.expiresAt) {
		return time.Time{}, fmt.Errorf("%w: expiry must be after the resolved start time", ErrInvalidArgument)
	}
	return startsAt, nil
}

func validateMutationContext(mutation MutationContext) error {
	if err := validateIdentifier("organization ID", string(mutation.OrganizationID)); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if err := validateIdentifier("actor user ID", mutation.ActorUserID); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if mutation.OperationID == uuid.Nil {
		return fmt.Errorf("%w: operation ID must not be nil", ErrInvalidArgument)
	}
	return nil
}

func validateExistingRequest(mutation MutationContext, prescriptionID PrescriptionID, expectedVersion int64) error {
	if err := validateMutationContext(mutation); err != nil {
		return err
	}
	if _, err := parsePrescriptionID(prescriptionID); err != nil {
		return err
	}
	if expectedVersion < 1 {
		return fmt.Errorf("%w: expected version must be positive", ErrInvalidArgument)
	}
	return nil
}

func parsePrescriptionID(id PrescriptionID) (uuid.UUID, error) {
	parsed, err := uuid.Parse(string(id))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: prescription ID must be a UUID", ErrInvalidArgument)
	}
	return parsed, nil
}

func databaseTime(ctx context.Context, queries *repo.Queries) (time.Time, error) {
	value, err := queries.GetKillswitchDatabaseTime(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("get killswitch database time: %w", err)
	}
	if !value.Valid {
		return time.Time{}, errors.New("killswitch database time is null")
	}
	return value.Time.UTC().Truncate(time.Microsecond), nil
}

func ensureIdentity(current lockedCurrent, identity prescriptionIdentity) error {
	if current.ID != identity.id || DefinitionKey(current.DefinitionKey) != identity.definition || PrincipalKind(current.PrincipalKind) != identity.principalKind || PrincipalKey(current.PrincipalKey) != identity.principalKey || ResourceKind(current.ResourceKind) != identity.resourceKind {
		return errors.New("killswitch prescription identity changed unexpectedly")
	}
	return nil
}

func validateStoredResourceScope(scope ResourceScope) error {
	if scope != ResourceScopeAll && scope != ResourceScopeSelected {
		return fmt.Errorf("unknown stored killswitch resource scope %q", scope)
	}
	return nil
}

func validateStoredResourceCount(scope ResourceScope, count int64) error {
	if err := validateStoredResourceScope(scope); err != nil {
		return err
	}
	if count > maxSelectedResourceCount {
		return fmt.Errorf("killswitch version has %d selected resources, exceeding the limit of %d", count, maxSelectedResourceCount)
	}
	if scope == ResourceScopeAll && count != 0 {
		return errors.New("all-resource killswitch version has selected resource rows")
	}
	if scope == ResourceScopeSelected && count == 0 {
		return errors.New("selected-resource killswitch version has no resource rows")
	}
	return nil
}

func changedSelectedResources(ctx context.Context, queries *repo.Queries, current lockedCurrent, desired canonicalDesiredVersion) ([]ResourceKey, error) {
	currentScope := ResourceScope(current.ResourceScope)
	if currentScope != desired.resourceScope {
		return desired.resourceKeys, nil
	}
	if currentScope == ResourceScopeAll {
		return nil, nil
	}
	resources, err := queries.ListKillswitchPrescriptionVersionResources(ctx, repo.ListKillswitchPrescriptionVersionResourcesParams{OrganizationID: current.OrganizationID, PrescriptionID: current.ID, Version: current.CurrentVersion})
	if err != nil {
		return nil, fmt.Errorf("list current killswitch resources: %w", err)
	}
	if err := validateStoredResourceCount(currentScope, int64(len(resources))); err != nil {
		return nil, err
	}
	if len(resources) == len(desired.resourceKeys) {
		equal := true
		for i := range resources {
			if ResourceKey(resources[i]) != desired.resourceKeys[i] {
				equal = false
				break
			}
		}
		if equal {
			return nil, nil
		}
	}
	return desired.resourceKeys, nil
}

func clonePostgresTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC().Truncate(time.Microsecond)
	return &normalized
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	utc := value.Time.UTC()
	return &utc
}

func resourceKeysToStrings(keys []ResourceKey) []string {
	result := make([]string, len(keys))
	for i, key := range keys {
		result[i] = string(key)
	}
	return result
}

func validPrescriptionState(state PrescriptionState) bool {
	return state == PrescriptionStateActive || state == PrescriptionStateInactive
}

// CleanupExpiredOperations deletes one bounded batch of expired operation receipts for the
// trusted organization. Production scheduling and privileged global sweeping belong elsewhere.
func (s *LifecycleService) CleanupExpiredOperations(ctx context.Context, organizationID OrganizationID, batchSize int32) (int64, error) {
	if err := validateIdentifier("organization ID", string(organizationID)); err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if batchSize < 1 || batchSize > maxCleanupBatchSize {
		return 0, fmt.Errorf("%w: cleanup batch size must be between 1 and %d", ErrInvalidArgument, maxCleanupBatchSize)
	}
	rows, err := repo.New(s.db).DeleteExpiredKillswitchOperations(ctx, repo.DeleteExpiredKillswitchOperationsParams{
		OrganizationID: string(organizationID),
		BatchSize:      batchSize,
	})
	if err != nil {
		return 0, fmt.Errorf("delete expired killswitch operations: %w", err)
	}
	return rows, nil
}
