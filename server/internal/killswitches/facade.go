package killswitches

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/killswitches/repo"
)

const MaxListPrescriptions = int32(100)

// GenericService is the transport-neutral lifecycle and current-state boundary.
// Capability-specific and separately authorized platform adapters are its only callers.
type GenericService interface {
	ListDefinitions(context.Context) ([]Definition, error)
	ActivatePrescription(context.Context, ActivatePrescriptionInput) (MutationResult, error)
	ChangePrescription(context.Context, ChangePrescriptionRequest) (MutationResult, error)
	DeactivatePrescription(context.Context, DeactivatePrescriptionRequest) (MutationResult, error)
	GetPrescription(context.Context, GetPrescriptionRequest) (CurrentPrescription, error)
	ListPrescriptions(context.Context, ListPrescriptionsRequest) (ListPrescriptionsResult, error)
}

// ActivatePrescriptionInput represents either creation or reactivation. A nil
// PrescriptionID creates; a non-nil PrescriptionID requires ExpectedVersion and
// reactivates the existing prescription.
type ActivatePrescriptionInput struct {
	MutationContext
	PrescriptionID  *PrescriptionID
	ExpectedVersion *int64
	Definition      DefinitionKey
	PrincipalKind   PrincipalKind
	PrincipalInput  string
	ResourceKind    ResourceKind
	Desired         DesiredVersionInput
}

type GetPrescriptionRequest struct {
	OrganizationID OrganizationID
	PrescriptionID PrescriptionID
}

type ListPrescriptionsRequest struct {
	OrganizationID OrganizationID
	Limit          int32
	AfterID        *PrescriptionID
}

type ListPrescriptionsResult struct {
	Prescriptions []CurrentPrescription
	NextAfterID   *PrescriptionID
}

// CurrentPrescription is the complete authorized current-version projection.
type CurrentPrescription struct {
	ID                   PrescriptionID
	OrganizationID       OrganizationID
	Definition           DefinitionKey
	PrincipalKind        PrincipalKind
	PrincipalKey         PrincipalKey
	ResourceKind         ResourceKind
	Version              int64
	State                PrescriptionState
	ResourceScope        ResourceScope
	SelectedResourceKeys []ResourceKey
	StartsAt             time.Time
	ExpiresAt            *time.Time
	ActivatedAt          *time.Time
	SupersededAt         *time.Time
	InternalNote         string
	ExternalNote         string
}

// Facade reuses LifecycleService for all mutation semantics and adds bounded,
// organization-qualified current-state reads. It has no evaluator dependency.
type Facade struct {
	lifecycle *LifecycleService
	registry  *Registry
	queries   *repo.Queries
}

var _ GenericService = (*Facade)(nil)

func NewFacade(lifecycle *LifecycleService) (*Facade, error) {
	if lifecycle == nil {
		return nil, ErrInvalidArgument
	}
	return &Facade{lifecycle: lifecycle, registry: lifecycle.registry, queries: repo.New(lifecycle.db)}, nil
}

func (f *Facade) ListDefinitions(_ context.Context) ([]Definition, error) {
	return f.registry.Definitions(), nil
}

func (f *Facade) ActivatePrescription(ctx context.Context, input ActivatePrescriptionInput) (MutationResult, error) {
	if input.PrescriptionID == nil {
		if input.ExpectedVersion != nil {
			return MutationResult{}, fmt.Errorf("%w: expected version is only valid for reactivation", ErrInvalidArgument)
		}
		return f.lifecycle.ActivatePrescription(ctx, ActivatePrescriptionRequest{
			MutationContext: input.MutationContext,
			Definition:      input.Definition,
			PrincipalKind:   input.PrincipalKind,
			PrincipalInput:  input.PrincipalInput,
			ResourceKind:    input.ResourceKind,
			Desired:         input.Desired,
		})
	}
	if input.ExpectedVersion == nil {
		return MutationResult{}, fmt.Errorf("%w: expected version is required for reactivation", ErrInvalidArgument)
	}
	if input.Definition != "" || input.PrincipalKind != "" || input.PrincipalInput != "" || input.ResourceKind != "" {
		return MutationResult{}, fmt.Errorf("%w: reactivation cannot replace prescription identity", ErrInvalidArgument)
	}
	return f.lifecycle.ReactivatePrescription(ctx, ReactivatePrescriptionRequest{
		MutationContext: input.MutationContext,
		PrescriptionID:  *input.PrescriptionID,
		ExpectedVersion: *input.ExpectedVersion,
		Desired:         input.Desired,
	})
}

func (f *Facade) ChangePrescription(ctx context.Context, request ChangePrescriptionRequest) (MutationResult, error) {
	return f.lifecycle.ChangePrescription(ctx, request)
}

func (f *Facade) DeactivatePrescription(ctx context.Context, request DeactivatePrescriptionRequest) (MutationResult, error) {
	return f.lifecycle.DeactivatePrescription(ctx, request)
}

func (f *Facade) GetPrescription(ctx context.Context, request GetPrescriptionRequest) (CurrentPrescription, error) {
	if err := validateIdentifier("organization ID", string(request.OrganizationID)); err != nil {
		return CurrentPrescription{}, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	prescriptionID, err := parsePrescriptionID(request.PrescriptionID)
	if err != nil {
		return CurrentPrescription{}, err
	}
	row, err := f.queries.GetKillswitchCurrentPrescription(ctx, repo.GetKillswitchCurrentPrescriptionParams{
		OrganizationID: string(request.OrganizationID),
		PrescriptionID: prescriptionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return CurrentPrescription{}, ErrPrescriptionNotFound
	}
	if err != nil {
		return CurrentPrescription{}, fmt.Errorf("get current killswitch prescription: %w", err)
	}
	return f.currentPrescription(currentPrescriptionRow{
		id: row.ID, organizationID: row.OrganizationID, definitionKey: row.DefinitionKey,
		principalKind: row.PrincipalKind, principalKey: row.PrincipalKey, resourceKind: row.ResourceKind,
		currentVersion: row.CurrentVersion, state: row.State, resourceScope: row.ResourceScope,
		startsAt: row.StartsAt, expiresAt: row.ExpiresAt, activatedAt: row.ActivatedAt, supersededAt: row.SupersededAt,
		internalNote: row.InternalNote, externalNote: row.ExternalNote,
		selectedResourceKeys: row.SelectedResourceKeys,
	})
}

func (f *Facade) ListPrescriptions(ctx context.Context, request ListPrescriptionsRequest) (ListPrescriptionsResult, error) {
	if err := validateIdentifier("organization ID", string(request.OrganizationID)); err != nil {
		return ListPrescriptionsResult{}, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	limit := request.Limit
	if limit == 0 {
		limit = MaxListPrescriptions
	}
	if limit < 1 || limit > MaxListPrescriptions {
		return ListPrescriptionsResult{}, fmt.Errorf("%w: list limit must be between 1 and %d", ErrInvalidArgument, MaxListPrescriptions)
	}
	afterID := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if request.AfterID != nil {
		parsed, err := parsePrescriptionID(*request.AfterID)
		if err != nil {
			return ListPrescriptionsResult{}, err
		}
		afterID = uuid.NullUUID{UUID: parsed, Valid: true}
		if _, err := f.queries.GetKillswitchCurrentPrescription(ctx, repo.GetKillswitchCurrentPrescriptionParams{OrganizationID: string(request.OrganizationID), PrescriptionID: parsed}); errors.Is(err, pgx.ErrNoRows) {
			return ListPrescriptionsResult{}, ErrPrescriptionNotFound
		} else if err != nil {
			return ListPrescriptionsResult{}, fmt.Errorf("validate killswitch prescription cursor: %w", err)
		}
	}
	rows, err := f.queries.ListKillswitchCurrentPrescriptions(ctx, repo.ListKillswitchCurrentPrescriptionsParams{
		OrganizationID: string(request.OrganizationID),
		AfterID:        afterID,
		ResultLimit:    limit + 1,
	})
	if err != nil {
		return ListPrescriptionsResult{}, fmt.Errorf("list current killswitch prescriptions: %w", err)
	}
	hasMore := len(rows) > int(limit)
	if hasMore {
		rows = rows[:limit]
	}
	prescriptions := make([]CurrentPrescription, 0, len(rows))
	for _, row := range rows {
		prescription, err := f.currentPrescription(currentPrescriptionRow{
			id: row.ID, organizationID: row.OrganizationID, definitionKey: row.DefinitionKey,
			principalKind: row.PrincipalKind, principalKey: row.PrincipalKey, resourceKind: row.ResourceKind,
			currentVersion: row.CurrentVersion, state: row.State, resourceScope: row.ResourceScope,
			startsAt: row.StartsAt, expiresAt: row.ExpiresAt, activatedAt: row.ActivatedAt, supersededAt: row.SupersededAt,
			internalNote: row.InternalNote, externalNote: row.ExternalNote, selectedResourceKeys: row.SelectedResourceKeys,
		})
		if err != nil {
			return ListPrescriptionsResult{}, err
		}
		prescriptions = append(prescriptions, prescription)
	}
	result := ListPrescriptionsResult{Prescriptions: prescriptions, NextAfterID: nil}
	if hasMore {
		next := prescriptions[len(prescriptions)-1].ID
		result.NextAfterID = &next
	}
	return result, nil
}

type currentPrescriptionRow struct {
	id                   uuid.UUID
	organizationID       string
	definitionKey        string
	principalKind        string
	principalKey         string
	resourceKind         string
	currentVersion       int64
	state                string
	resourceScope        string
	startsAt             pgtype.Timestamptz
	expiresAt            pgtype.Timestamptz
	activatedAt          pgtype.Timestamptz
	supersededAt         pgtype.Timestamptz
	internalNote         string
	externalNote         string
	selectedResourceKeys []string
}

func (f *Facade) currentPrescription(row currentPrescriptionRow) (CurrentPrescription, error) {
	startsAt := optionalTime(row.startsAt)
	if startsAt == nil {
		return CurrentPrescription{}, errors.New("current killswitch prescription has no start time")
	}
	state := PrescriptionState(row.state)
	if !validPrescriptionState(state) {
		return CurrentPrescription{}, fmt.Errorf("unknown stored killswitch prescription state %q", state)
	}
	scope := ResourceScope(row.resourceScope)
	if err := validateStoredResourceCount(scope, int64(len(row.selectedResourceKeys))); err != nil {
		return CurrentPrescription{}, err
	}
	resourceKeys := make([]ResourceKey, len(row.selectedResourceKeys))
	for i, resource := range row.selectedResourceKeys {
		resourceKeys[i] = ResourceKey(resource)
	}
	return CurrentPrescription{
		ID: PrescriptionID(row.id.String()), OrganizationID: OrganizationID(row.organizationID),
		Definition: DefinitionKey(row.definitionKey), PrincipalKind: PrincipalKind(row.principalKind),
		PrincipalKey: PrincipalKey(row.principalKey), ResourceKind: ResourceKind(row.resourceKind),
		Version: row.currentVersion, State: state, ResourceScope: scope,
		SelectedResourceKeys: resourceKeys, StartsAt: *startsAt, ExpiresAt: optionalTime(row.expiresAt),
		ActivatedAt: optionalTime(row.activatedAt), SupersededAt: optionalTime(row.supersededAt),
		InternalNote: row.internalNote, ExternalNote: row.externalNote,
	}, nil
}
