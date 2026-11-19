package repo

import (
	"context"
	"fmt"
)

// chatLogTimeBoundsCompleteFromUnixNano (2026-11-26T00:00:00Z) is the earliest
// chat start that chat_log_time_bounds is trusted to cover in full. Logs
// inserted before chat_log_time_bounds_mv existed are missing from the table
// until an operator backfill runs, so a chat that started earlier could report
// a first log later than its real one. Such chats keep the caller's lower
// bound. The upper bound is always safe: every chat with a bounds row has had
// its latest logs inserted through the view. If the view ships after this
// instant, move the constant past the deploy.
const chatLogTimeBoundsCompleteFromUnixNano int64 = 1795651200000000000

type chatLogTimeBounds struct {
	first int64
	last  int64
	found bool
}

func (q *Queries) getChatLogTimeBounds(ctx context.Context, projectID, chatID string) (chatLogTimeBounds, error) {
	query, args, err := sq.Select(
		"min(first_time_unix_nano)",
		"max(last_time_unix_nano)",
		"count()",
	).
		From("chat_log_time_bounds").
		Where("gram_project_id = ?", projectID).
		Where("gram_chat_id = ?", chatID).
		ToSql()
	if err != nil {
		return chatLogTimeBounds{}, fmt.Errorf("building chat log time bounds query: %w", err)
	}

	rows, err := q.conn.Query(ctx, query, args...)
	if err != nil {
		return chatLogTimeBounds{}, fmt.Errorf("querying chat log time bounds: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var bounds chatLogTimeBounds
	var count uint64
	if rows.Next() {
		if err := rows.Scan(&bounds.first, &bounds.last, &count); err != nil {
			return chatLogTimeBounds{}, fmt.Errorf("scanning chat log time bounds: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return chatLogTimeBounds{}, fmt.Errorf("reading chat log time bounds: %w", err)
	}
	bounds.found = count > 0
	return bounds, nil
}

// narrow intersects [timeStart, timeEnd] with the chat's recorded log range.
func (b chatLogTimeBounds) narrow(timeStart, timeEnd int64) (int64, int64) {
	if !b.found {
		return timeStart, timeEnd
	}
	timeEnd = min(timeEnd, b.last)
	if b.first >= chatLogTimeBoundsCompleteFromUnixNano {
		timeStart = max(timeStart, b.first)
	}
	return timeStart, timeEnd
}
