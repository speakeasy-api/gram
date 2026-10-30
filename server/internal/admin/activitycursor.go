package admin

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
)

// activityCursor is a position in each of the two logs the staff activity
// view merges. Each log pages by its own seq, so one seq cannot describe both.
type activityCursor struct {
	configuration activityLogPosition
	security      activityLogPosition
}

// activityLogPosition is where the next page of one log starts: below seq when
// it is set, from the newest entry when it is not, and nowhere once the log
// has no entries left.
type activityLogPosition struct {
	seq  pgtype.Int8
	done bool
}

const activityCursorPrefix = "activity:"

func (c activityCursor) encode() string {
	payload := activityCursorPrefix + c.configuration.encode() + ":" + c.security.encode()
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func (p activityLogPosition) encode() string {
	switch {
	case p.done:
		return "done"
	case p.seq.Valid:
		return strconv.FormatInt(p.seq.Int64, 10)
	default:
		return ""
	}
}

// ValidateActivityCursor reports whether cursor has the staff activity cursor
// format, for callers that check input before listing.
func ValidateActivityCursor(cursor string) error {
	_, err := decodeActivityCursor(cursor)
	return err
}

func decodeActivityCursor(cursor string) (activityCursor, error) {
	var none activityCursor
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return none, fmt.Errorf("decode activity cursor: %w", err)
	}
	payload, ok := strings.CutPrefix(string(decoded), activityCursorPrefix)
	if !ok {
		return none, fmt.Errorf("invalid activity cursor format")
	}
	configuration, security, ok := strings.Cut(payload, ":")
	if !ok {
		return none, fmt.Errorf("invalid activity cursor format")
	}
	configurationPosition, err := decodeActivityLogPosition(configuration)
	if err != nil {
		return none, err
	}
	securityPosition, err := decodeActivityLogPosition(security)
	if err != nil {
		return none, err
	}
	return activityCursor{configuration: configurationPosition, security: securityPosition}, nil
}

func decodeActivityLogPosition(value string) (activityLogPosition, error) {
	switch value {
	case "":
		return activityLogPosition{seq: pgtype.Int8{Int64: 0, Valid: false}, done: false}, nil
	case "done":
		return activityLogPosition{seq: pgtype.Int8{Int64: 0, Valid: false}, done: true}, nil
	}
	seq, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return activityLogPosition{}, fmt.Errorf("invalid activity cursor sequence: %w", err)
	}
	return activityLogPosition{seq: pgtype.Int8{Int64: seq, Valid: true}, done: false}, nil
}

type activityEntry struct {
	row      auditrepo.ListAuditLogsRow
	category audit.Category
}

// mergeActivity interleaves one fetched page of each log newest first and
// returns up to pageSize entries, with the cursor for the next page or nil
// when both logs are exhausted. Each fetched slice is in its log's seq order
// and holds at most pageSize+1 rows, so a log that returned fewer than that
// and was fully taken has nothing further. Configuration entries come first
// when timestamps tie.
func mergeActivity(configuration, security []auditrepo.ListAuditLogsRow, cursor activityCursor, pageSize int) ([]activityEntry, *activityCursor) {
	page := make([]activityEntry, 0, pageSize)
	ci, si := 0, 0
	for len(page) < pageSize && (ci < len(configuration) || si < len(security)) {
		takeConfiguration := si >= len(security) ||
			(ci < len(configuration) && !configuration[ci].CreatedAt.Time.Before(security[si].CreatedAt.Time))
		if takeConfiguration {
			page = append(page, activityEntry{row: configuration[ci], category: audit.CategoryConfiguration})
			ci++
		} else {
			page = append(page, activityEntry{row: security[si], category: audit.CategorySecurity})
			si++
		}
	}

	next := activityCursor{
		configuration: advanceActivityLog(cursor.configuration, configuration, ci, pageSize),
		security:      advanceActivityLog(cursor.security, security, si, pageSize),
	}
	if next.configuration.done && next.security.done {
		return page, nil
	}
	return page, &next
}

func advanceActivityLog(position activityLogPosition, rows []auditrepo.ListAuditLogsRow, taken, pageSize int) activityLogPosition {
	if position.done || (taken == len(rows) && len(rows) <= pageSize) {
		return activityLogPosition{seq: pgtype.Int8{Int64: 0, Valid: false}, done: true}
	}
	if taken == 0 {
		return position
	}
	return activityLogPosition{seq: pgtype.Int8{Int64: rows[taken-1].Seq, Valid: true}, done: false}
}
