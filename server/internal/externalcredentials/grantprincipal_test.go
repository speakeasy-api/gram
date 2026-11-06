package externalcredentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/externalcredentials"
)

func TestParseCustomerGrantPrincipal(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		value string
		want  string
		err   bool
	}{
		{name: "unset", value: "  ", want: "", err: false},
		{name: "group", value: "group:Speakeasy-KMS@example.com", want: "group:speakeasy-kms@example.com", err: false},
		{name: "service account", value: "serviceAccount:gram@gram-platform.iam.gserviceaccount.com", want: "serviceAccount:gram@gram-platform.iam.gserviceaccount.com", err: false},
		{name: "bare email", value: "speakeasy-kms@example.com", want: "", err: true},
		{name: "user member", value: "user:someone@example.com", want: "", err: true},
		{name: "missing domain", value: "group:speakeasy-kms@", want: "", err: true},
		{name: "missing local part", value: "group:@example.com", want: "", err: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := externalcredentials.ParseCustomerGrantPrincipal(tc.value)
			if tc.err {
				require.ErrorIs(t, err, externalcredentials.ErrInvalidGrantPrincipal)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
