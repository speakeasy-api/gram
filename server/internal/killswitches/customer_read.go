package killswitches

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/killswitches/repo"
)

type CustomerStatus string

const (
	CustomerStatusActive    CustomerStatus = "active"
	CustomerStatusScheduled CustomerStatus = "scheduled"
	CustomerStatusExpired   CustomerStatus = "expired"
	CustomerStatusLifted    CustomerStatus = "lifted"
)

type CustomerStartMode string

const (
	CustomerStartModeNow       CustomerStartMode = "now"
	CustomerStartModeScheduled CustomerStartMode = "scheduled"
)

type CustomerListCursor struct {
	CreatedAt time.Time
	ID        PrescriptionID
}

type ListCustomerPrescriptionsRequest struct {
	OrganizationID OrganizationID
	Definition     DefinitionKey
	PrincipalKind  PrincipalKind
	ResourceKind   ResourceKind
	PrincipalKey   *PrincipalKey
	Status         *CustomerStatus
	Limit          int32
	Cursor         *CustomerListCursor
	StatusAsOf     time.Time
}

type AuthorizedListCustomerPrescriptionsRequest struct {
	Definition    DefinitionKey
	PrincipalKind PrincipalKind
	ResourceKind  ResourceKind
	PrincipalKey  *PrincipalKey
	Status        *CustomerStatus
	Limit         int32
	Cursor        *CustomerListCursor
	StatusAsOf    time.Time
}

type CustomerListItem struct {
	ID                   PrescriptionID
	CreatedAt            time.Time
	PrincipalKey         PrincipalKey
	Version              int64
	Status               CustomerStatus
	StartMode            CustomerStartMode
	ResourceScope        ResourceScope
	SelectedResourceKeys []ResourceKey
	StartsAt             time.Time
	ExpiresAt            *time.Time
}

type ListCustomerPrescriptionsResult struct {
	Items      []CustomerListItem
	NextCursor *CustomerListCursor
}

type customerReadService interface {
	ListCustomerPrescriptions(context.Context, ListCustomerPrescriptionsRequest) (ListCustomerPrescriptionsResult, error)
}

func (f *Facade) ListCustomerPrescriptions(ctx context.Context, request ListCustomerPrescriptionsRequest) (ListCustomerPrescriptionsResult, error) {
	if request.OrganizationID == "" || request.Definition == "" || request.PrincipalKind == "" || request.ResourceKind == "" || request.StatusAsOf.IsZero() || request.Limit < 1 || request.Limit > MaxListPrescriptions {
		return ListCustomerPrescriptionsResult{}, ErrInvalidArgument
	}
	params := repo.ListCustomerKillswitchesParams{
		OrganizationID: string(request.OrganizationID),
		DefinitionKey:  string(request.Definition),
		PrincipalKind:  string(request.PrincipalKind),
		ResourceKind:   string(request.ResourceKind),
		ResultLimit:    request.Limit + 1,
		StatusAsOf:     pgtype.Timestamptz{Time: request.StatusAsOf, Valid: true, InfinityModifier: pgtype.Finite},
		CustomerStatus: pgtype.Text{String: "", Valid: false}, UserID: pgtype.Text{String: "", Valid: false},
		CursorCreatedAt: pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite}, CursorID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
	}
	if request.PrincipalKey != nil {
		params.UserID = pgtype.Text{String: string(*request.PrincipalKey), Valid: true}
	}
	if request.Status != nil {
		if !validCustomerStatus(*request.Status) {
			return ListCustomerPrescriptionsResult{}, ErrInvalidArgument
		}
		params.CustomerStatus = pgtype.Text{String: string(*request.Status), Valid: true}
	}
	if request.Cursor != nil {
		id, err := uuid.Parse(string(request.Cursor.ID))
		if err != nil || id == uuid.Nil || id.String() != string(request.Cursor.ID) || request.Cursor.CreatedAt.IsZero() {
			return ListCustomerPrescriptionsResult{}, ErrInvalidArgument
		}
		params.CursorCreatedAt = pgtype.Timestamptz{Time: request.Cursor.CreatedAt, Valid: true, InfinityModifier: pgtype.Finite}
		params.CursorID = uuid.NullUUID{UUID: id, Valid: true}
	}

	rows, err := repo.New(f.db).ListCustomerKillswitches(ctx, params)
	if err != nil {
		return ListCustomerPrescriptionsResult{}, fmt.Errorf("list customer killswitches: %w", err)
	}
	hasMore := len(rows) > int(request.Limit)
	if hasMore {
		rows = rows[:request.Limit]
	}
	items := make([]CustomerListItem, 0, len(rows))
	for _, row := range rows {
		if !row.CreatedAt.Valid || !row.StartsAt.Valid {
			return ListCustomerPrescriptionsResult{}, errors.New("customer killswitch row has invalid timestamps")
		}
		status := CustomerStatus(row.CustomerStatus)
		if !validCustomerStatus(status) {
			return ListCustomerPrescriptionsResult{}, fmt.Errorf("unknown stored customer killswitch status %q", status)
		}
		startMode := CustomerStartMode(row.CustomerStart)
		if startMode != CustomerStartModeNow && startMode != CustomerStartModeScheduled {
			return ListCustomerPrescriptionsResult{}, fmt.Errorf("unknown stored customer start mode %q", startMode)
		}
		scope := ResourceScope(row.ResourceScope)
		if err := validateStoredResourceCount(scope, int64(len(row.SelectedResourceKeys))); err != nil {
			return ListCustomerPrescriptionsResult{}, err
		}
		items = append(items, CustomerListItem{
			ID: PrescriptionID(row.ID.String()), CreatedAt: row.CreatedAt.Time, PrincipalKey: PrincipalKey(row.UserID),
			Version: row.Version, Status: status, StartMode: startMode, ResourceScope: scope,
			SelectedResourceKeys: resourceKeys(row.SelectedResourceKeys), StartsAt: row.StartsAt.Time, ExpiresAt: optionalTime(row.ExpiresAt),
		})
	}
	result := ListCustomerPrescriptionsResult{Items: items, NextCursor: nil}
	if hasMore {
		last := items[len(items)-1]
		result.NextCursor = &CustomerListCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	return result, nil
}

func validCustomerStatus(status CustomerStatus) bool {
	switch status {
	case CustomerStatusActive, CustomerStatusScheduled, CustomerStatusExpired, CustomerStatusLifted:
		return true
	default:
		return false
	}
}

func resourceKeys(values []string) []ResourceKey {
	keys := make([]ResourceKey, len(values))
	for i, value := range values {
		keys[i] = ResourceKey(value)
	}
	return keys
}
