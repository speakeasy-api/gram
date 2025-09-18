package log

import (
	"log/slog"
	"os"
)

var L = slog.New(slog.NewTextHandler(os.Stderr, nil))
