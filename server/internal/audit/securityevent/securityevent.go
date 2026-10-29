// Package securityevent records security activity that is not a configuration
// decision, such as a runtime admitting its own workload identity on every
// start, in security_audit_logs.
//
// Configuration changes made by an organization's members or the Speakeasy
// team belong in the audit package and its audit_logs table, which customers
// read as their configuration history. Security activity happens far more
// often, so it is kept in its own table. The same action can belong to either
// log: an admin admitting a workload is a configuration change, a runtime
// admitting itself is a security event. The caller decides by choosing the
// package.
//
// Entries carry the same columns as audit_logs, are stamped with the same
// acting surface and client, and publish the same webhook events with category
// "security".
package securityevent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/securityevent/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Subject types share their values with the audit package so both logs filter
// and render a subject alike.
const subjectTypeWorkloadAdmission = "workload_admission"

// record writes one security entry and publishes its webhook event in the
// caller's transaction.
func record(ctx context.Context, dbtx repo.DBTX, params repo.InsertSecurityAuditLogParams, event *outbox.EventDef[events.AuditLogCreatedPayloadV1]) error {
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type == urn.PrincipalTypeAgent {
		params.ActorID = actor.ID
		params.ActorType = string(actor.Type)
		params.ActorDisplayName = conv.ToPGTextEmpty("")
		params.ActorSlug = conv.ToPGTextEmpty("")
	}
	clientID, _ := contextvalues.GetOAuthClientID(ctx)
	params.ActingSurface = conv.ToPGText(string(audit.SurfaceFromContext(ctx)))
	params.ActingClientID = conv.ToPGTextEmpty(clientID)

	row, err := repo.New(dbtx).InsertSecurityAuditLog(ctx, params)
	if err != nil {
		return fmt.Errorf("log security event %s: %w", params.Action, err)
	}

	actorID := params.ActorID
	actorDisplayName := conv.FromPGTextOrEmpty[string](params.ActorDisplayName)
	actorSlug := conv.FromPGTextOrEmpty[string](params.ActorSlug)
	actingClientID := clientID
	if audit.IsStaffAdminSurface(audit.Surface(conv.FromPGTextOrEmpty[string](params.ActingSurface))) {
		actorID = ""
		actorDisplayName = audit.SpeakeasyTeamActorLabel
		actorSlug = ""
		actingClientID = ""
	}
	if _, err := outbox.PublishWebhookEvent(ctx, dbtx, row.OrganizationID, event, events.AuditLogCreatedPayloadV1{
		ID:                 row.ID,
		OrganizationID:     row.OrganizationID,
		ProjectID:          params.ProjectID,
		ActorID:            actorID,
		ActorType:          params.ActorType,
		ActorDisplayName:   actorDisplayName,
		ActorSlug:          actorSlug,
		Action:             params.Action,
		SubjectID:          params.SubjectID,
		SubjectType:        params.SubjectType,
		SubjectDisplayName: conv.FromPGTextOrEmpty[string](params.SubjectDisplayName),
		SubjectSlug:        conv.FromPGTextOrEmpty[string](params.SubjectSlug),
		BeforeSnapshot:     params.BeforeSnapshot,
		AfterSnapshot:      params.AfterSnapshot,
		Metadata:           params.Metadata,
		ActingSurface:      conv.FromPGTextOrEmpty[string](params.ActingSurface),
		Category:           string(audit.CategorySecurity),
		ActingClientID:     actingClientID,
	}); err != nil {
		return fmt.Errorf("log security event %s: append to outbox: %w", params.Action, err)
	}
	return nil
}

// marshalSnapshot encodes a snapshot, leaving a nil one as SQL NULL.
func marshalSnapshot[T any](snapshot *T) ([]byte, error) {
	if snapshot == nil {
		return nil, nil
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot: %w", err)
	}
	return b, nil
}
