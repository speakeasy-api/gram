package relay

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	maxDebugLogBytes       = 10 << 20
	maxDebugLogRecordBytes = 64 << 10
)

type debugLogWriter struct {
	path string
}

func newDebugLogger(path string) *slog.Logger {
	var writer io.Writer = io.Discard
	if path != "" {
		writer = debugLogWriter{path: path}
	}
	return slog.New(slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})).With(
		"component", "speakeasy-hooks",
		"version", BinaryVersion,
		"pid", os.Getpid(),
	)
}

// Write appends one complete slog record. Hook invocations are separate
// processes, so the sibling lock keeps their records intact and serializes
// rotation. One previous file is retained to bound diagnostics to about 20 MiB.
func (w debugLogWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	requested := len(p)
	if len(p) > maxDebugLogRecordBytes {
		const suffix = "... record truncated\n"
		truncated := make([]byte, 0, maxDebugLogRecordBytes)
		truncated = append(truncated, p[:maxDebugLogRecordBytes-len(suffix)]...)
		truncated = append(truncated, suffix...)
		p = truncated
	}

	parent := filepath.Dir(w.path)
	if parent != "." {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return 0, fmt.Errorf("create hooks log directory: %w", err)
		}
	}

	var (
		written  int
		writeErr error
	)
	withFileLock(w.path, func() {
		if info, err := os.Stat(w.path); err == nil && info.Size()+int64(len(p)) > maxDebugLogBytes {
			if err := os.Remove(w.path + ".1"); err != nil && !os.IsNotExist(err) {
				writeErr = fmt.Errorf("remove previous hooks log: %w", err)
				return
			}
			if err := os.Rename(w.path, w.path+".1"); err != nil {
				writeErr = fmt.Errorf("rotate hooks log: %w", err)
				return
			}
		}

		file, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			writeErr = fmt.Errorf("open hooks log: %w", err)
			return
		}
		if err := secureLogFile(file); err != nil {
			_ = file.Close()
			writeErr = fmt.Errorf("secure hooks log permissions: %w", err)
			return
		}
		written, writeErr = file.Write(p)
		if closeErr := file.Close(); writeErr == nil && closeErr != nil {
			writeErr = fmt.Errorf("close hooks log: %w", closeErr)
		}
	})
	if writeErr == nil && written == len(p) {
		return requested, nil
	}
	return written, writeErr
}
