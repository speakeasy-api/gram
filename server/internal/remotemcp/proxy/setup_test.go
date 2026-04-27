package proxy_test

import "log/slog"

// discardLogger returns a slog.Logger that discards all output, used in tests
// where log content is not being asserted against.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discardWriter{}, nil))
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
