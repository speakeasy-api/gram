package risk

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/repo"
)

// Risk listings name a chat as the chat pages do, so a cron chat that stored
// its raw envelope reads under its job name.
func TestDisplayChatTitleFollowsTheChatTitleRules(t *testing.T) {
	t.Parallel()

	envelope := pgtype.Text{String: "[cron:0f0e0d0c-0b0a-4908-8706-050403020100 weekly-skill-review] Review", Valid: true}
	title := displayChatTitle(repo.ListChatTitlesByIDsRow{ID: uuid.New(), Title: envelope, TitleManuallySet: false})
	require.NotNil(t, title)
	require.Equal(t, "Cron: weekly-skill-review", *title)

	manual := displayChatTitle(repo.ListChatTitlesByIDsRow{ID: uuid.New(), Title: envelope, TitleManuallySet: true})
	require.NotNil(t, manual)
	require.Equal(t, envelope.String, *manual)

	require.Nil(t, displayChatTitle(repo.ListChatTitlesByIDsRow{ID: uuid.New(), Title: pgtype.Text{String: "", Valid: false}, TitleManuallySet: false}))
}
