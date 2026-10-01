package attr

import "log/slog"

// SlogSigintEventID identifies the logical event, independently of transport delivery.
func SlogSigintEventID(v string) slog.Attr { return slog.String("gram.sigint.event.id", v) }

// SlogSigintEventKind namespaces the event identity within its tenant.
func SlogSigintEventKind(v string) slog.Attr { return slog.String("gram.sigint.event.kind", v) }
