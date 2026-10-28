package audit

import (
	"context"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/audit/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/outbox"
	"github.com/speakeasy-api/gram/server/internal/outbox/events"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

type Logger struct {
	skipOutbox bool
}

type LoggerOption func(*Logger)

// WithoutOutbox records audit rows without publishing webhook events, for operator backfills.
func WithoutOutbox() LoggerOption {
	return func(l *Logger) { l.skipOutbox = true }
}

func NewLogger(opts ...LoggerOption) *Logger {
	l := &Logger{skipOutbox: false}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

type auditEntry struct {
	Params      repo.InsertAuditLogParams
	OutboxEvent *outbox.EventDef[events.AuditLogCreatedPayloadV1]
}

// log writes one audit record.
//
// Acting surface and client are stamped here rather than by each event, so
// every audited mutation carries them and no call site can forget or disagree.
// They describe the request, not the event, which is why they are derived from
// the context at the moment of the write.
func (l *Logger) log(ctx context.Context, dbtx repo.DBTX, entry auditEntry) error {
	applyAuthenticatedActor(ctx, &entry.Params)
	identity := actingIdentityFromContext(ctx)
	// The column is nullable so that rows predating it need no backfill, but
	// nothing written from here on is left null: an unattributable write
	// records SurfaceUnknown explicitly. A null surface therefore means "older
	// than attribution", which is a different statement from "unidentifiable".
	entry.Params.ActingSurface = conv.ToPGText(string(identity.Surface))
	// A call with no OAuth client records NULL, not an empty string: absent and
	// blank are different answers to "which client acted".
	entry.Params.ActingClientID = conv.ToPGTextEmpty(identity.ClientID)

	// Most writers never set a category, so an entry records a configuration
	// change unless its Log function says otherwise.
	switch Category(entry.Params.Category) {
	case "":
		entry.Params.Category = string(CategoryConfiguration)
	case CategoryConfiguration, CategorySecurity:
	default:
		return fmt.Errorf("log %s: unknown audit category %q", entry.Params.Action, entry.Params.Category)
	}

	row, err := repo.New(dbtx).InsertAuditLog(ctx, entry.Params)
	if err != nil {
		return fmt.Errorf("log %s: %w", entry.Params.Action, err)
	}

	if l.skipOutbox {
		return nil
	}
	if err := appendToOutbox(ctx, dbtx, entry, row); err != nil {
		return err
	}

	return nil
}

func applyAuthenticatedActor(ctx context.Context, params *repo.InsertAuditLogParams) {
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	if !ok || actor.Type != urn.PrincipalTypeAgent {
		return
	}
	params.ActorID = actor.ID
	params.ActorType = string(actor.Type)
	params.ActorDisplayName = conv.ToPGTextEmpty("")
	params.ActorSlug = conv.ToPGTextEmpty("")
}
