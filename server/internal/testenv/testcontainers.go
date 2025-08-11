package testenv

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/testcontainers/testcontainers-go/log"

	"github.com/speakeasy-api/gram/server/internal/o11y"
)

type testcontainersLogger struct {
	logger *slog.Logger
}

func (t *testcontainersLogger) Printf(format string, v ...any) {
	t.logger.Log(context.Background(), slog.LevelInfo, fmt.Sprintf(format, v...))
}

func NewTestcontainersLogger() log.Logger {
	return &testcontainersLogger{
		logger: slog.New(o11y.NewLogHandler(&o11y.LogHandlerOptions{
			RawLevel:    os.Getenv("LOG_LEVEL"),
			Pretty:      true,
			DataDogAttr: false,
		})),
	}
}
