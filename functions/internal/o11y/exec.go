package o11y

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"
)

func CaptureRawLogLines(ctx context.Context, logger *slog.Logger, rdr io.Reader, attr ...slog.Attr) error {
	scanner := bufio.NewScanner(rdr)
	handler := logger.Handler()

	for scanner.Scan() {
		bs := scanner.Bytes()
		rec := slog.NewRecord(time.Now(), slog.LevelInfo, "log", 0)
		rec.AddAttrs(slog.String("data", string(bs)))
		rec.AddAttrs(attr...)

		// not being able to handle a log line is non-fatal
		_ = handler.Handle(ctx, rec)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan command output: %w", err)
	}

	return nil
}
