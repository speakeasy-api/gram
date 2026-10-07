package attr

import "log/slog"

// SlogSigintSensorID identifies the sensor configuration responsible for an evaluation.
func SlogSigintSensorID(v string) slog.Attr { return slog.String("gram.sigint.sensor.id", v) }

// SlogSigintEventID identifies the logical event, independently of transport delivery.
func SlogSigintEventID(v string) slog.Attr { return slog.String("gram.sigint.event.id", v) }

// SlogSigintEventKind namespaces the event identity within its tenant.
func SlogSigintEventKind(v string) slog.Attr { return slog.String("gram.sigint.event.kind", v) }
