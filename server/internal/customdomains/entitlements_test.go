package customdomains

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCanCreateCustomDomain(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		accountType string
		want        bool
	}{
		{accountType: "free", want: false},
		{accountType: "pro", want: true},
		{accountType: "payg", want: true},
		{accountType: "enterprise", want: true},
		{accountType: "unknown", want: false},
	} {
		t.Run(tt.accountType, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, canCreateCustomDomain(tt.accountType))
		})
	}
}
