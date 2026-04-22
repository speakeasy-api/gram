package enforceo11yconventions

import stdslog "log/slog"
import "time"

type logger struct{}

func (logger) String(string, string) {}

func bad() {
	_ = stdslog.String("key", "value")          // want "avoid direct slog attribute constructors"
	_ = stdslog.Int64("count", 1)               // want "avoid direct slog attribute constructors"
	_ = stdslog.Group("group")                  // want "avoid direct slog attribute constructors"
	_ = stdslog.GroupAttrs("group")             // want "avoid direct slog attribute constructors"
	_ = stdslog.Duration("timeout", 0)          // want "avoid direct slog attribute constructors"
	_ = stdslog.Float64("ratio", 1)             // want "avoid direct slog attribute constructors"
	_ = stdslog.Bool("enabled", true)           // want "avoid direct slog attribute constructors"
	_ = stdslog.Uint64("size", 1)               // want "avoid direct slog attribute constructors"
	_ = stdslog.Time("created_at", time.Time{}) // want "avoid direct slog attribute constructors"
	_ = stdslog.Any("payload", struct{}{})      // want "avoid direct slog attribute constructors"
	_ = stdslog.Int("answer", 42)               // want "avoid direct slog attribute constructors"
}

func good() {
	local := logger{}
	local.String("key", "value")
	String("key", "value")
	v := stdslog.StringValue("value")
	_ = v.String()
	_ = stdslog.Attr{}

	a := stdslog.Attr{}
	_ = a.Value.String()

	r := stdslog.Record{}
	_ = r.Level.String()
}

func String(string, string) {}
