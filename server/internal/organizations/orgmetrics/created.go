// Package orgmetrics counts organizations as they are created, labelled by
// the flow that inserted the organization_metadata row.
package orgmetrics

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/organizations/orgprovision"
)

const (
	meterScope = "github.com/speakeasy-api/gram/server/internal/organizations/orgmetrics"

	meterOrganizationsCreated = "gram.organizations.created"
)

// CreationSource is the flow that inserted an organization_metadata row. The
// values are a metric contract that monitors query, so they are spelled here
// rather than borrowed from the creation_source column: renaming a stored
// value must not silently rename a metric label.
type CreationSource string

const (
	// CreationSourceSignup is a self-serve signup, including a partner-code
	// signup.
	CreationSourceSignup CreationSource = "signup"

	// CreationSourceAssistants is the organization auto-provisioned for a user
	// who arrived through Assistants.
	CreationSourceAssistants CreationSource = "assistants"

	// CreationSourcePlatformMCP is a self-serve signup that started from an MCP
	// client.
	CreationSourcePlatformMCP CreationSource = "platform_mcp"

	// CreationSourcePlatformAdmin is an organization a platform admin created
	// from the admin dashboard.
	CreationSourcePlatformAdmin CreationSource = "platform_admin"

	// CreationSourceWorkOSSync is a row the worker inserted while applying a
	// WorkOS organization event.
	CreationSourceWorkOSSync CreationSource = "workos_sync"

	// CreationSourceLoginMirror is a row inserted while mirroring a user's
	// WorkOS memberships at login, for an organization not yet known locally.
	CreationSourceLoginMirror CreationSource = "login_mirror"

	// creationSourceUnknown is any value outside the closed set. Record drops
	// it rather than emitting a label no monitor knows about.
	creationSourceUnknown CreationSource = ""
)

// ProvisioningSource maps an orgprovision.Source* value onto its metric
// source. Any other value maps to creationSourceUnknown, which Record drops.
func ProvisioningSource(source string) CreationSource {
	switch source {
	case orgprovision.SourceSignup:
		return CreationSourceSignup
	case orgprovision.SourceAssistants:
		return CreationSourceAssistants
	case orgprovision.SourcePlatformMCP:
		return CreationSourcePlatformMCP
	case orgprovision.SourcePlatformAdmin:
		return CreationSourcePlatformAdmin
	default:
		return creationSourceUnknown
	}
}

func (s CreationSource) known() bool {
	switch s {
	case CreationSourceSignup, CreationSourceAssistants, CreationSourcePlatformMCP,
		CreationSourcePlatformAdmin, CreationSourceWorkOSSync, CreationSourceLoginMirror:
		return true
	default:
		return false
	}
}

// Created counts organization_metadata rows that were actually inserted.
// Callers record only after the transaction that inserted the row commits,
// and only when the write reported an insert rather than an update, so a
// rollback, a retried activity, or a writer that lost an insert race to
// another flow does not count.
type Created struct {
	// logger reports a creation recorded with a source outside the closed set.
	logger *slog.Logger

	// created is nil when the instrument failed to register, and Record then
	// does nothing.
	created metric.Int64Counter
}

func NewCreated(logger *slog.Logger, meterProvider metric.MeterProvider) *Created {
	meter := meterProvider.Meter(meterScope)

	created, err := meter.Int64Counter(
		meterOrganizationsCreated,
		metric.WithDescription("Organizations created, by the flow that inserted the organization."),
		metric.WithUnit("{organization}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterOrganizationsCreated), attr.SlogError(err))
	}

	return &Created{logger: logger, created: created}
}

// Record counts one created organization. It never carries organization or
// user identifiers, and drops a source outside the closed set rather than
// emitting an unknown label.
func (m *Created) Record(ctx context.Context, source CreationSource) {
	if m == nil || m.created == nil {
		return
	}
	if !source.known() {
		if m.logger != nil {
			m.logger.ErrorContext(ctx, "organization created with unknown creation source", attr.SlogOrganizationCreationSource(source))
		}
		return
	}
	m.created.Add(ctx, 1, metric.WithAttributes(attr.OrganizationCreationSource(source)))
}
