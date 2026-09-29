package admin

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
)

func TestRegistryErrorLogsUnexpectedCause(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		err    error
		logged bool
	}{
		{"unexpected", errors.New("database unavailable"), true},
		{"not found", mcpregistry.ErrNotFound, false},
		{"conflict", mcpregistry.ErrConflict, false},
		{"invalid cursor", mcpregistry.ErrInvalidCursor, false},
		{"success", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			s := &Service{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			err := s.registryError(context.Background(), tc.err)
			if (err == nil) != (tc.err == nil) {
				t.Fatal("error presence changed")
			}
			if got := logs.Len() != 0; got != tc.logged {
				t.Fatalf("logged = %v, want %v", got, tc.logged)
			}
			if tc.logged && !strings.Contains(logs.String(), tc.err.Error()) {
				t.Fatal("missing original cause")
			}
		})
	}
}
