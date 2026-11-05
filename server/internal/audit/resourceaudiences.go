package audit

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	ActionAccessResourceAudienceUpdate Action = "access_resource_audience:update"
)

// resourceAudienceAuditMetadata says, without decoding the snapshots, how the
// save changed who can connect: connecting is the access the audience exists
// to decide, and the other levels are reported only in the snapshots.
type resourceAudienceAuditMetadata struct {
	ConnectGranted   []string `json:"connect_granted"`
	ConnectRevoked   []string `json:"connect_revoked"`
	ConnectBlocked   []string `json:"connect_blocked"`
	ConnectUnblocked []string `json:"connect_unblocked"`
}

type LogAccessResourceAudienceUpdateEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID

	Actor            urn.Principal
	ActorDisplayName *string
	ActorSlug        *string

	// The audience belongs to one MCP resource, which may be a toolset, a
	// remote or unproxied server, or a gateway. No one URN type spans them.
	ResourceID string //nolint:glint // auditeventurnnaming: an MCP resource id spans toolsets, servers and gateways, so no single URN type names it

	// The rules naming this resource before and after the save. Rules
	// covering every resource are owned by the role editor and never change
	// here, so they are not part of either snapshot.
	AudienceSnapshotBefore []*accessgen.SetResourceAudienceEntry
	AudienceSnapshotAfter  []*accessgen.SetResourceAudienceEntry
}

func (l *Logger) LogAccessResourceAudienceUpdate(ctx context.Context, dbtx repo.DBTX, event LogAccessResourceAudienceUpdateEvent) error {
	action := ActionAccessResourceAudienceUpdate

	beforeSnapshot, err := marshalAuditPayload(event.AudienceSnapshotBefore)
	if err != nil {
		return fmt.Errorf("marshal %s before snapshot: %w", action, err)
	}
	afterSnapshot, err := marshalAuditPayload(event.AudienceSnapshotAfter)
	if err != nil {
		return fmt.Errorf("marshal %s after snapshot: %w", action, err)
	}
	metadata, err := marshalAuditPayload(resourceAudienceConnectChanges(event.AudienceSnapshotBefore, event.AudienceSnapshotAfter))
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

		SubjectID:          event.ResourceID,
		SubjectType:        string(subjectTypeAccessResourceAudience),
		SubjectDisplayName: conv.ToPGTextEmpty(""),
		SubjectSlug:        conv.ToPGTextEmpty(""),

		BeforeSnapshot: beforeSnapshot,
		AfterSnapshot:  afterSnapshot,
		Metadata:       metadata,
	}

	return l.log(ctx, dbtx, auditEntry{Params: entry, OutboxEvent: events.AccessResourceAudienceV1})
}

// resourceAudienceConnectChanges lists the principals whose whole-server
// connect rule this save added or removed. Narrowed rules are left to the
// snapshots: "connect to two tools" is neither a grant nor a revocation of
// the server.
func resourceAudienceConnectChanges(before, after []*accessgen.SetResourceAudienceEntry) resourceAudienceAuditMetadata {
	wholeServer := func(entries []*accessgen.SetResourceAudienceEntry, level string) map[string]struct{} {
		set := make(map[string]struct{})
		for _, entry := range entries {
			if entry == nil || entry.Level != level || len(entry.Tools) > 0 || len(entry.Dispositions) > 0 {
				continue
			}
			set[entry.PrincipalUrn] = struct{}{}
		}
		return set
	}
	added := func(from, to map[string]struct{}) []string {
		out := make([]string, 0)
		for principal := range to {
			if _, ok := from[principal]; !ok {
				out = append(out, principal)
			}
		}
		slices.Sort(out)
		return out
	}

	useBefore, useAfter := wholeServer(before, "use"), wholeServer(after, "use")
	blockBefore, blockAfter := wholeServer(before, "blocked"), wholeServer(after, "blocked")
	return resourceAudienceAuditMetadata{
		ConnectGranted:   added(useBefore, useAfter),
		ConnectRevoked:   added(useAfter, useBefore),
		ConnectBlocked:   added(blockBefore, blockAfter),
		ConnectUnblocked: added(blockAfter, blockBefore),
	}
}
