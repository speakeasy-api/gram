package repo

import "github.com/Masterminds/squirrel"

func mappedSkillTelemetryQuery(projectIDs []string, timeStart, timeEnd int64, mappedSessions squirrel.SelectBuilder) squirrel.SelectBuilder {
	return sq.Select().
		From("telemetry_logs").
		Where(squirrel.Eq{"gram_project_id": projectIDs}).
		Where("time_unix_nano >= ?", timeStart).
		Where("time_unix_nano <= ?", timeEnd).
		Where(skillVersionSourceRowPredicate).
		Where("chat_id != ''").
		// Keep chat_id as a standalone predicate so ClickHouse can use its
		// skipping index. The project and surface join still decides attribution.
		Where(squirrel.Expr("chat_id IN (?)", mappedSessions))
}
