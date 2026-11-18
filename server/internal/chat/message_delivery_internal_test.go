package chat

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSameAccount_ComparesTheFirstIdentityBothHave(t *testing.T) {
	t.Parallel()
	account := uuid.New()
	tests := []struct {
		name   string
		owner  TranscriptAccount
		sender TranscriptAccount
		want   bool
	}{
		{"same AI account", TranscriptAccount{account, "a", ""}, TranscriptAccount{account, "b", ""}, true},
		{"different AI account wins over the same user", TranscriptAccount{account, "a", "x@example.test"}, TranscriptAccount{uuid.New(), "a", "x@example.test"}, false},
		{"user when one side has no AI account", TranscriptAccount{account, "a", ""}, TranscriptAccount{uuid.Nil, "a", ""}, true},
		{"different user", TranscriptAccount{uuid.Nil, "a", "x@example.test"}, TranscriptAccount{uuid.Nil, "b", "x@example.test"}, false},
		{"email ignores case and space", TranscriptAccount{uuid.Nil, "", "X@Example.test"}, TranscriptAccount{uuid.Nil, "b", " x@example.test"}, true},
		{"different email", TranscriptAccount{uuid.Nil, "", "x@example.test"}, TranscriptAccount{uuid.Nil, "", "y@example.test"}, false},
		{"nothing to compare", TranscriptAccount{account, "", ""}, TranscriptAccount{uuid.Nil, "a", "x@example.test"}, false},
		{"no identity", TranscriptAccount{uuid.Nil, "", ""}, TranscriptAccount{uuid.Nil, "", ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, sameAccount(tt.owner, tt.sender))
		})
	}
}
