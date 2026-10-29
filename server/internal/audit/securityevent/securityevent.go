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
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/audit/securityevent/repo"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

// Subject types share their values with the audit package so both logs filter
// and render a subject alike.
const subjectTypeWorkloadAdmission = "workload_admission"

// record writes one security entry and publishes its webhook event in the
// caller's transaction. Request attribution and the webhook payload come from
// the audit package, so both logs stamp and publish entries alike; the two
// tables share their columns, which is what lets the params convert.
func record(ctx context.Context, dbtx repo.DBTX, params repo.InsertSecurityAuditLogParams, event *outbox.EventDef[events.AuditLogCreatedPayloadV1]) error {
	entry := auditrepo.InsertAuditLogParams(params)
	audit.StampRequest(ctx, &entry)

	row, err := repo.New(dbtx).InsertSecurityAuditLog(ctx, repo.InsertSecurityAuditLogParams(entry))
	if err != nil {
		return fmt.Errorf("log security event %s: %w", entry.Action, err)
	}
	if err := audit.PublishWebhook(ctx, dbtx, entry, auditrepo.InsertAuditLogRow(row), audit.CategorySecurity, event); err != nil {
		return fmt.Errorf("publish security event: %w", err)
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
