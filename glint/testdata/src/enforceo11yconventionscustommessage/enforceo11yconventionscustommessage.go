package enforceo11yconventionscustommessage

import "log/slog"

func bad() {
	_ = slog.String("key", "value") // want "avoid direct slog attribute constructors: use attr[.]Slog[*] helpers instead"
}
