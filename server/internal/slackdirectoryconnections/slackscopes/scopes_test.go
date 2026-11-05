package slackscopes_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/slackdirectoryconnections/slackscopes"
	"github.com/stretchr/testify/require"
)

func TestMissing(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{slackscopes.ChatWrite, slackscopes.AppMentionsRead, slackscopes.IMHistory}, slackscopes.Missing([]string{"users:read", "users:read.email"}))
	require.Equal(t, []string{slackscopes.AppMentionsRead, slackscopes.IMHistory}, slackscopes.Missing([]string{"users:read", "chat:write", "users:read.email"}))
	require.Empty(t, slackscopes.Missing([]string{"users:read", "chat:write", "users:read.email", "app_mentions:read", "im:history", "team:read"}))
	require.Equal(t, slackscopes.Requested, slackscopes.Missing(nil))
}
