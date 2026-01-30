// Package plog provides pretty-printing for JSON lines logs with colorized output.
package plog

import (
	"bufio"
	"fmt"
	"io"
)

// PrettyPrint reads JSON lines from r and writes formatted output using the given options.
func PrettyPrint(r io.Reader, opts ...Option) error {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	formatter := NewFormatter(cfg)
	scanner := bufio.NewScanner(r)

	// Increase buffer size for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		record, err := Parse(line, cfg)
		if err != nil {
			// Not valid JSON, pass through as-is
			if _, err := cfg.Output.Write(line); err != nil {
				return fmt.Errorf("writing line: %w", err)
			}
			if _, err := cfg.Output.Write([]byte("\n")); err != nil {
				return fmt.Errorf("writing newline: %w", err)
			}
			continue
		}

		// Skip records below minimum level
		if record.Level < cfg.Level {
			continue
		}

		if err := formatter.Format(cfg.Output, record); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanning input: %w", err)
	}
	return nil
}
