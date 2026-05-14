package toolsets

import (
	"errors"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestValidateAutoSyncSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []string
		wantErr bool
		errCode oops.Code
	}{
		{
			name:    "empty list is valid",
			entries: []string{},
		},
		{
			name:    "nil is valid",
			entries: nil,
		},
		{
			name:    "single function entry",
			entries: []string{"function:my-tools"},
		},
		{
			name:    "multiple function entries",
			entries: []string{"function:my-tools", "function:internal"},
		},
		{
			name:    "function entry with hyphens and dots",
			entries: []string{"function:acme.foo-bar"},
		},
		{
			name:    "rejects http kind (reserved for future PR)",
			entries: []string{"http:my-spec"},
			wantErr: true,
			errCode: oops.CodeBadRequest,
		},
		{
			name:    "rejects externalmcp kind",
			entries: []string{"externalmcp:github"},
			wantErr: true,
			errCode: oops.CodeBadRequest,
		},
		{
			name:    "rejects unknown kind",
			entries: []string{"banana:source"},
			wantErr: true,
			errCode: oops.CodeBadRequest,
		},
		{
			name:    "rejects empty kind",
			entries: []string{":my-source"},
			wantErr: true,
			errCode: oops.CodeBadRequest,
		},
		{
			name:    "rejects empty source",
			entries: []string{"function:"},
			wantErr: true,
			errCode: oops.CodeBadRequest,
		},
		{
			name:    "rejects missing colon",
			entries: []string{"function-my-source"},
			wantErr: true,
			errCode: oops.CodeBadRequest,
		},
		{
			name:    "rejects mixed valid+invalid (one bad apple fails the whole list)",
			entries: []string{"function:good", "http:bad"},
			wantErr: true,
			errCode: oops.CodeBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateAutoSyncSources(tc.entries)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				var shared *oops.ShareableError
				if !errors.As(err, &shared) {
					t.Fatalf("expected *oops.ShareableError, got %T", err)
				}
				if shared.Code != tc.errCode {
					t.Fatalf("expected error code %q, got %q", tc.errCode, shared.Code)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
