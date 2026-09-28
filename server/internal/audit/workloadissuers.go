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
	ActionWorkloadIssuerCreate Action = "workload-issuer:create"
	ActionWorkloadIssuerDelete Action = "workload-issuer:delete"
)

// WorkloadIssuerSnapshot is the audited state of a trusted workload issuer.
//
// Every field in it is security-relevant. JwksURI is the key the verification
// path fetches, so repointing it repoints who can mint a workload session, and
// AllowWildcardAdmission is what lets a single rule stand for a fleet. Recording
// them is what makes a change reviewable as a diff rather than as a bare event.
type WorkloadIssuerSnapshot struct {
	Name                   string `json:"name"`
	Issuer                 string `json:"issuer"`
	JwksURI                string `json:"jwks_uri"`
	AllowWildcardAdmission bool   `json:"allow_wildcard_admission"`
	// Tier is "organization" or "project". Recorded because the same issuer name
	// can exist at both, and the tier decides who the issuer is trusted by.
	Tier string `json:"tier"`
}

type LogWorkloadIssuerCreateEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	IssuerURN  urn.WorkloadIssuer
	IssuerName string

	IssuerSnapshotAfter *WorkloadIssuerSnapshot
}

func (l *Logger) LogWorkloadIssuerCreate(ctx context.Context, dbtx repo.DBTX, event LogWorkloadIssuerCreateEvent) error {
	action := ActionWorkloadIssuerCreate

	after, err := marshalAuditPayload(event.IssuerSnapshotAfter)
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

		SubjectID:          event.IssuerURN.ID.String(),
		SubjectType:        string(subjectTypeWorkloadIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.IssuerName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: nil,
		AfterSnapshot:  after,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.WorkloadIssuerV1})
}

type LogWorkloadIssuerDeleteEvent struct {
	OrganizationID string
	ProjectID      uuid.NullUUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	IssuerURN  urn.WorkloadIssuer
	IssuerName string

	IssuerSnapshotBefore *WorkloadIssuerSnapshot
}

func (l *Logger) LogWorkloadIssuerDelete(ctx context.Context, dbtx repo.DBTX, event LogWorkloadIssuerDeleteEvent) error {
	action := ActionWorkloadIssuerDelete

	before, err := marshalAuditPayload(event.IssuerSnapshotBefore)
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

		SubjectID:          event.IssuerURN.ID.String(),
		SubjectType:        string(subjectTypeWorkloadIssuer),
		SubjectDisplayName: conv.ToPGTextEmpty(event.IssuerName),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       nil,
		BeforeSnapshot: before,
		AfterSnapshot:  nil,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.WorkloadIssuerV1})
}
