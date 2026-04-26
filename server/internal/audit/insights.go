package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	subjectTypeInsightsProposal subjectType = "insights_proposal"
	subjectTypeInsightsMemory   subjectType = "insights_memory"
)

const (
	ActionInsightsProposalApplied    Action = "insights.proposal.applied"
	ActionInsightsProposalDismissed  Action = "insights.proposal.dismissed"
	ActionInsightsProposalRolledBack Action = "insights.proposal.rolled_back"
	ActionInsightsMemoryForgotten    Action = "insights.memory.forgotten"
)

type LogInsightsProposalEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	ProposalID    string
	ProposalKind  string
	TargetRef     string
	BeforeSnapshot any
	AfterSnapshot  any
}

func logInsightsProposalEvent(ctx context.Context, dbtx repo.DBTX, action Action, event LogInsightsProposalEvent) error {
	beforeSnapshot, err := marshalAuditPayload(event.BeforeSnapshot)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAuditPayload(event.AfterSnapshot)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	metadata, err := marshalAuditPayload(map[string]any{
		"kind":       event.ProposalKind,
		"target_ref": event.TargetRef,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.ProposalID,
		SubjectType:        string(subjectTypeInsightsProposal),
		SubjectDisplayName: conv.ToPGTextEmpty(event.TargetRef),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}
	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

func LogInsightsProposalApplied(ctx context.Context, dbtx repo.DBTX, event LogInsightsProposalEvent) error {
	return logInsightsProposalEvent(ctx, dbtx, ActionInsightsProposalApplied, event)
}

func LogInsightsProposalDismissed(ctx context.Context, dbtx repo.DBTX, event LogInsightsProposalEvent) error {
	return logInsightsProposalEvent(ctx, dbtx, ActionInsightsProposalDismissed, event)
}

func LogInsightsProposalRolledBack(ctx context.Context, dbtx repo.DBTX, event LogInsightsProposalEvent) error {
	return logInsightsProposalEvent(ctx, dbtx, ActionInsightsProposalRolledBack, event)
}

type LogInsightsMemoryForgottenEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	MemoryID      string
	MemoryKind    string
	MemoryContent string
}

func LogInsightsMemoryForgotten(ctx context.Context, dbtx repo.DBTX, event LogInsightsMemoryForgottenEvent) error {
	action := ActionInsightsMemoryForgotten

	metadata, err := marshalAuditPayload(map[string]any{
		"kind":    event.MemoryKind,
		"content": event.MemoryContent,
	})
	if err != nil {
		return fmt.Errorf("marshal %s metadata: %w", action, err)
	}

	entry := repo.InsertAuditLogParams{
		OrganizationID: event.OrganizationID,
		ProjectID:      uuid.NullUUID{UUID: event.ProjectID, Valid: event.ProjectID != uuid.Nil},

		ActorID:          event.Actor.ID,
		ActorType:        string(event.Actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(event.ActorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(event.ActorSlug),

		Action: string(action),

		SubjectID:          event.MemoryID,
		SubjectType:        string(subjectTypeInsightsMemory),
		SubjectDisplayName: conv.ToPGTextEmpty(event.MemoryKind),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		Metadata:       metadata,
		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
	}
	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}
