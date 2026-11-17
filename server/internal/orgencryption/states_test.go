package orgencryption_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

func TestIsUnreadableAndIsKeyProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		unreadable bool
		keyProblem bool
	}{
		{name: "unreadable", err: orgencryption.ErrUnreadable, unreadable: true, keyProblem: false},
		{name: "missing", err: orgencryption.ErrMissing, unreadable: true, keyProblem: false},
		{name: "suspended", err: orgencryption.ErrSuspended, unreadable: false, keyProblem: true},
		{name: "key unavailable", err: orgencryption.ErrKeyUnavailable, unreadable: false, keyProblem: true},
		{name: "fenced", err: orgencryption.ErrFenced, unreadable: false, keyProblem: true},
		{name: "decision expired", err: orgencryption.ErrDecisionExpired, unreadable: false, keyProblem: true},
		{name: "no organization encryption", err: orgencryption.ErrOrganizationEncryptionUnavailable, unreadable: false, keyProblem: true},
		{name: "wrapped suspended", err: fmt.Errorf("open token: %w", orgencryption.ErrSuspended), unreadable: false, keyProblem: true},
		{name: "database failure", err: errors.New("connection reset"), unreadable: false, keyProblem: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tc.unreadable, orgencryption.IsUnreadable(tc.err))
			require.Equal(t, tc.keyProblem, orgencryption.IsKeyProblem(tc.err))
		})
	}
}
