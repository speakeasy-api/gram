package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func legacyAliasEndpointsEnv(key string) string {
	if key == "GRAM_DATABASE_URL" {
		return "postgres://user@db.example.internal:5432/gram"
	}
	return ""
}

func TestParseLegacyAliasEndpointsFlagsDryRunByDefault(t *testing.T) {
	t.Parallel()
	cfg, err := parseLegacyAliasEndpointsFlags(nil, legacyAliasEndpointsEnv)
	require.NoError(t, err)
	require.True(t, cfg.dryRun)
}

func TestParseLegacyAliasEndpointsFlagsWriteGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "missing journal", args: []string{"-dry-run=false", "-confirm-target=db.example.internal:5432/gram"}, wantErr: "-journal"},
		{name: "wrong target", args: []string{"-dry-run=false", "-journal=j.jsonl", "-confirm-target=other:5432/gram"}, wantErr: "-confirm-target=db.example.internal:5432/gram"},
		{name: "invalid project", args: []string{"-project=nope"}, wantErr: "invalid -project"},
		{name: "confirmed", args: []string{"-dry-run=false", "-journal=j.jsonl", "-confirm-target=db.example.internal:5432/gram"}, wantErr: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseLegacyAliasEndpointsFlags(tc.args, legacyAliasEndpointsEnv)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
