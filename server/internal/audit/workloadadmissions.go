package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// Admitting a subject is the grant of machine access and withdrawing it is
	// the revocation, so the verbs are named for what they do rather than as
	// create and delete.
	ActionWorkloadAdmissionAdmit    Action = "workload-admission:admit"
	ActionWorkloadAdmissionUpdate   Action = "workload-admission:update"
	ActionWorkloadAdmissionWithdraw Action = "workload-admission:withdraw"
)

// KitRuntimeActorID is the system principal a Kit assistant runtime records
// when it admits and withdraws its own workload identity.
const KitRuntimeActorID = "kit-runtime"

// KitRuntimeAdmittedSubjects reports which of the given admissions the Kit
// runtime admitted for itself, according to the audit trail. Entries about
// those admissions are operational and recorded as internal.
func KitRuntimeAdmittedSubjects(ctx context.Context, dbtx repo.DBTX, organizationID string, admissionIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	if len(admissionIDs) == 0 {
		return map[uuid.UUID]bool{}, nil
	}
	ids := make([]string, 0, len(admissionIDs))
	for _, id := range admissionIDs {
		ids = append(ids, id.String())
	}
	rows, err := repo.New(dbtx).ListKitRuntimeAdmittedSubjects(ctx, repo.ListKitRuntimeAdmittedSubjectsParams{OrganizationID: organizationID, SubjectIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list kit runtime admitted subjects: %w", err)
	}
	admitted := make(map[uuid.UUID]bool, len(rows))
	for _, row := range rows {
		id, err := uuid.Parse(row)
		if err != nil {
			return nil, fmt.Errorf("parse kit runtime admitted subject: %w", err)
		}
		admitted[id] = true
	}
	return admitted, nil
}

// WorkloadAdmissionSnapshot is the audited state of one admitted subject.
//
// MatchKind and Subject together are the breadth of the grant: a wildcard rule
// stands for every subject under its stem, so reading the subject alone would
// understate what was admitted. AssignedAgentID is recorded because the agent
// supplies the whole policy an admitted workload acts under — changing it
// changes what the machine may reach without touching the admission.
type WorkloadAdmissionSnapshot struct {
	Issuer     string `json:"issuer"`
	IssuerName string `json:"issuer_name"`
	Subject    string `json:"subject"`
	MatchKind  string `json:"match_kind"`

	// Name is the operator's optional label; empty when none is set.
	Name string `json:"name"`

	// Tags are the operator's free-form labels; empty rather than absent.
	Tags []string `json:"tags"`

	AssignedAgentID string `json:"assigned_agent_id"`
	// Tier is "organization" or "project". A subject can be admitted at both
	// independently, and withdrawing one leaves the other in force, so an entry
	// that does not say which tier it describes cannot be acted on.
	Tier string `json:"tier"`
}

type LogWorkloadAdmissionAdmitEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AdmissionURN urn.WorkloadAdmission
	// Subject rather than a label: most platforms' subjects are self-describing
	// and the optional name is frequently absent.
	AdmissionDisplayName string

	AdmissionSnapshotAfter *WorkloadAdmissionSnapshot

	// Visibility is VisibilityInternal when Speakeasy admits or withdraws a
	// workload on its own, as a runtime does for itself on every start and
	// teardown. Empty means VisibilityCustomer.
	Visibility Visibility
}

func (l *Logger) LogWorkloadAdmissionAdmit(ctx context.Context, dbtx repo.DBTX, event LogWorkloadAdmissionAdmitEvent) error {
	action := ActionWorkloadAdmissionAdmit

	after, err := marshalAuditPayload(event.AdmissionSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.AdmissionURN.ID.String(),
		SubjectType:        string(subjectTypeWorkloadAdmission),
		SubjectDisplayName: conv.ToPGTextEmpty(event.AdmissionDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  after,

		Visibility: string(event.Visibility),
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.WorkloadAdmissionV1})
}

type LogWorkloadAdmissionUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AdmissionURN         urn.WorkloadAdmission
	AdmissionDisplayName string

	AdmissionSnapshotBefore *WorkloadAdmissionSnapshot
	AdmissionSnapshotAfter  *WorkloadAdmissionSnapshot
}

func (l *Logger) LogWorkloadAdmissionUpdate(ctx context.Context, dbtx repo.DBTX, event LogWorkloadAdmissionUpdateEvent) error {
	action := ActionWorkloadAdmissionUpdate

	before, err := marshalAuditPayload(event.AdmissionSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	after, err := marshalAuditPayload(event.AdmissionSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.AdmissionURN.ID.String(),
		SubjectType:        string(subjectTypeWorkloadAdmission),
		SubjectDisplayName: conv.ToPGTextEmpty(event.AdmissionDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: before,
		AfterSnapshot:  after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.WorkloadAdmissionV1})
}

type LogWorkloadAdmissionWithdrawEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	AdmissionURN         urn.WorkloadAdmission
	AdmissionDisplayName string

	AdmissionSnapshotBefore *WorkloadAdmissionSnapshot

	// Visibility is VisibilityInternal when Speakeasy admits or withdraws a
	// workload on its own, as a runtime does for itself on every start and
	// teardown. Empty means VisibilityCustomer.
	Visibility Visibility
}

func (l *Logger) LogWorkloadAdmissionWithdraw(ctx context.Context, dbtx repo.DBTX, event LogWorkloadAdmissionWithdrawEvent) error {
	action := ActionWorkloadAdmissionWithdraw

	before, err := marshalAuditPayload(event.AdmissionSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.AdmissionURN.ID.String(),
		SubjectType:        string(subjectTypeWorkloadAdmission),
		SubjectDisplayName: conv.ToPGTextEmpty(event.AdmissionDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: before,
		AfterSnapshot:  nil,

		Visibility: string(event.Visibility),
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.WorkloadAdmissionV1})
}
