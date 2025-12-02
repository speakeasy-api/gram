package o11y

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/speakeasy-api/gram/functions/internal/attr"
)

func CaptureRawLogLines(ctx context.Context, logger *slog.Logger, rdr io.Reader, attrs ...slog.Attr) error {
	scanner := bufio.NewScanner(rdr)
	handler := logger.Handler()

	for scanner.Scan() {
		bs := scanner.Bytes()
		rec := slog.NewRecord(time.Now(), slog.LevelInfo, "log", 0)
		rec.AddAttrs(attrs...)
		rec.AddAttrs(attr.SlogEventPayload(string(bs)))

		// not being able to handle a log line is non-fatal
		_ = handler.Handle(ctx, rec)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan command output: %w", err)
	}

	return nil
}
