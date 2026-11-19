package chat_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/risk/chatfindings"
	riskchrepo "github.com/speakeasy-api/gram/server/internal/risk/chrepo"
)

// unreachableClickHouse fails every read the way a ClickHouse that refuses
// connections does.
type unreachableClickHouse struct{}

var errClickHouseUnreachable = errors.New("handshake: read: i/o timeout")

func (unreachableClickHouse) Exec(context.Context, string, ...any) error {
	return errClickHouseUnreachable
}

func (unreachableClickHouse) Query(context.Context, string, ...any) (driver.Rows, error) {
	return nil, errClickHouseUnreachable
}

// TestListChats_RiskCountsUnavailable_OmitsCounts verifies that the chat list
// still loads when ClickHouse cannot answer the risk count read, and that the
// counts come back absent rather than as a misleading zero.
func TestListChats_RiskCountsUnavailable_OmitsCounts(t *testing.T) {
	t.Parallel()
	ti := newTestChatService(t)
	ctx := externalUserCtx(t, ti, "ext-ch-down")

	chatID := seedChat(t, ctx, ti, "", "ext-ch-down", "chat with risk")
	seedRiskOnChat(t, ctx, ti, chatID)

	ti.service.WithRiskFindings(chatfindings.New(ti.conn, riskchrepo.New(unreachableClickHouse{})))

	result, err := ti.service.ListChats(ctx, defaultPayload())
	require.NoError(t, err)
	require.Len(t, result.Chats, 1)
	require.Equal(t, chatID.String(), result.Chats[0].ID)
	require.Nil(t, result.Chats[0].RiskFindingsCount)
}
