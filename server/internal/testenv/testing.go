package testenv

import (
	"log/slog"
	"os"
	"testing"

	"github.com/speakeasy-api/gram/internal/o11y"
)

func NewLogger(*testing.T) *slog.Logger {
	if testing.Verbose() {
		return slog.New(o11y.NewLogHandler(os.Getenv("LOG_LEVEL"), true))
	} else {
		return slog.New(slog.DiscardHandler)
	}
}
