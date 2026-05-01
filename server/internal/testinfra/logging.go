package testinfra

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/testcontainers/testcontainers-go/log"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type testcontainersLogger struct {
	logger *slog.Logger
}

func (t *testcontainersLogger) Printf(format string, v ...any) {
	t.logger.Log(context.Background(), slog.LevelInfo, fmt.Sprintf(format, v...))
}

func NewTestcontainersLogger(rawLevel string) log.Logger {
	return &testcontainersLogger{
		logger: slog.New(o11y.NewLogHandler(&o11y.LogHandlerOptions{
			RawLevel:    rawLevel,
			Pretty:      true,
			DataDogAttr: false,
		})),
	}
}
