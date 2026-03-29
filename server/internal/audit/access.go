package audit

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	gen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionAccessGrantUpsert          Action = "access_grant:upsert"
	ActionAccessGrantRemove          Action = "access_grant:remove"
	ActionAccessGrantRemovePrincipal Action = "access_grant:remove_principal"
)

type LogAccessGrantUpsertEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	GrantBefore *gen.Grant
	GrantAfter  *gen.Grant
}

func LogAccessGrantUpsert(ctx context.Context, dbtx repo.DBTX, event LogAccessGrantUpsertEvent) error {
	action := ActionAccessGrantUpsert

	// Access logs every upsert attempt. Even when the grant tuple already exists,
	// the caller expressed the intent to grant access and the upsert refreshes
	// updated_at, so we keep a before/after trace.
	beforeSnapshot, err := marshalAccessGrantSnapshot(event.GrantBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	afterSnapshot, err := marshalAccessGrantSnapshot(event.GrantAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}

	entry := accessGrantAuditEntry(action, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, accessGrantSubject(event.GrantBefore, event.GrantAfter))
	entry.BeforeSnapshot = beforeSnapshot
	entry.AfterSnapshot = afterSnapshot
	entry.Metadata = nil

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogAccessGrantRemoveEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	Grant *gen.Grant
}

func LogAccessGrantRemove(ctx context.Context, dbtx repo.DBTX, event LogAccessGrantRemoveEvent) error {
	action := ActionAccessGrantRemove

	beforeSnapshot, err := marshalAccessGrantSnapshot(event.Grant)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	entry := accessGrantAuditEntry(action, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.Grant)
	entry.BeforeSnapshot = beforeSnapshot
	entry.AfterSnapshot = nil
	entry.Metadata = nil

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

type LogAccessGrantRemovePrincipalEvent struct {
	OrganizationID string

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	Grant *gen.Grant
}

func LogAccessGrantRemovePrincipal(ctx context.Context, dbtx repo.DBTX, event LogAccessGrantRemovePrincipalEvent) error {
	action := ActionAccessGrantRemovePrincipal

	beforeSnapshot, err := marshalAccessGrantSnapshot(event.Grant)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}

	entry := accessGrantAuditEntry(action, event.OrganizationID, event.Actor, event.ActorDisplayName, event.ActorSlug, event.Grant)
	entry.BeforeSnapshot = beforeSnapshot
	entry.AfterSnapshot = nil
	entry.Metadata = nil

	if _, err := repo.New(dbtx).InsertAuditLog(ctx, entry); err != nil {
		return fmt.Errorf("log %s: %w", action, err)
	}

	return nil
}

func accessGrantAuditEntry(action Action, organizationID string, actor urn.Principal, actorDisplayName, actorSlug *string, grant *gen.Grant) repo.InsertAuditLogParams {
	return repo.InsertAuditLogParams{
		OrganizationID: organizationID,
		ProjectID:      uuid.NullUUID{UUID: uuid.Nil, Valid: false},

		ActorID:          actor.ID,
		ActorType:        string(actor.Type),
		ActorDisplayName: conv.PtrToPGTextEmpty(actorDisplayName),
		ActorSlug:        conv.PtrToPGTextEmpty(actorSlug),

		Action: string(action),

		SubjectID:          grant.ID,
		SubjectType:        string(subjectTypeAccessGrant),
		SubjectDisplayName: conv.ToPGTextEmpty(grant.PrincipalUrn),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: nil,
		AfterSnapshot:  nil,
		Metadata:       nil,
	}
}

func accessGrantSubject(before, after *gen.Grant) *gen.Grant {
	if after != nil {
		return after
	}

	return before
}

func marshalAccessGrantSnapshot(grant *gen.Grant) ([]byte, error) {
	if grant == nil {
		return nil, nil
	}

	return marshalAuditPayload(grant)
}
