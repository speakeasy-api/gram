package securityevent

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/securityevent/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

// LogWorkloadAdmissionAdmit records Speakeasy admitting a workload on its own,
// as a Kit runtime admits its own workload identity on every start. An
// administrator admitting a workload is a configuration change and goes
// through audit.Logger.LogWorkloadAdmissionAdmit instead.
func LogWorkloadAdmissionAdmit(ctx context.Context, dbtx repo.DBTX, event audit.LogWorkloadAdmissionAdmitEvent) error {
	action := audit.ActionWorkloadAdmissionAdmit
	after, err := marshalSnapshot(event.AdmissionSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	return record(ctx, dbtx, repo.InsertSecurityAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.AdmissionURN.ID.String(),
		SubjectType:        subjectTypeWorkloadAdmission,
		SubjectDisplayName: conv.ToPGTextEmpty(event.AdmissionDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  after,

		ActingSurface:  conv.ToPGTextEmpty(""),
		ActingClientID: conv.ToPGTextEmpty(""),
	}, events.WorkloadAdmissionV1)
}

// LogWorkloadAdmissionWithdraw records the withdrawal of an admission a runtime
// created for itself, whether the runtime withdrew it on teardown or an
// administrator's issuer withdrawal cascaded to it.
func LogWorkloadAdmissionWithdraw(ctx context.Context, dbtx repo.DBTX, event audit.LogWorkloadAdmissionWithdrawEvent) error {
	action := audit.ActionWorkloadAdmissionWithdraw
	before, err := marshalSnapshot(event.AdmissionSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	return record(ctx, dbtx, repo.InsertSecurityAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      event.ProjectID,

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.AdmissionURN.ID.String(),
		SubjectType:        subjectTypeWorkloadAdmission,
		SubjectDisplayName: conv.ToPGTextEmpty(event.AdmissionDisplayName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: before,
		AfterSnapshot:  nil,

		ActingSurface:  conv.ToPGTextEmpty(""),
		ActingClientID: conv.ToPGTextEmpty(""),
	}, events.WorkloadAdmissionV1)
}

// RuntimeAdmittedSubjects reports which of the given admissions a Kit runtime
// admitted for itself, according to the audit trail, so their cascaded
// withdrawals are recorded here too.
func RuntimeAdmittedSubjects(ctx context.Context, dbtx repo.DBTX, organizationID string, admissionIDs []uuid.UUID) (map[uuid.UUID]bool, error) {
	if len(admissionIDs) == 0 {
		return map[uuid.UUID]bool{}, nil
	}
	ids := make([]string, 0, len(admissionIDs))
	for _, id := range admissionIDs {
		ids = append(ids, id.String())
	}
	rows, err := repo.New(dbtx).ListRuntimeAdmittedSubjects(ctx, repo.ListRuntimeAdmittedSubjectsParams{OrganizationID: organizationID, SubjectIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list runtime admitted subjects: %w", err)
	}
	admitted := make(map[uuid.UUID]bool, len(rows))
	for _, row := range rows {
		id, err := uuid.Parse(row)
		if err != nil {
			return nil, fmt.Errorf("parse runtime admitted subject: %w", err)
		}
		admitted[id] = true
	}
	return admitted, nil
}
