-- create "risk_finding_states" table
CREATE TABLE `gram`.`risk_finding_states` (
  `organization_id` String COMMENT 'Organization the finding belongs to.
@access: confidential' CODEC(ZSTD(1)),
  `project_id` String COMMENT 'Project the finding was scoped to. Empty string when unknown.
@access: confidential' CODEC(ZSTD(1)),
  `id` UUID COMMENT 'Finding id that every copy of the finding shares.
@access: confidential',
  `live` AggregateFunction(argMax, UInt8, Tuple(UInt8, DateTime64(9))) COMMENT '1 while the finding is neither suppressed, a false positive nor a could-not-analyze sentinel.
@access: confidential',
  `category` AggregateFunction(argMaxIf, String, Tuple(UInt8, DateTime64(9)), UInt8) COMMENT 'Risk category of the rule that fired, e.g. secrets.
@access: confidential',
  `rule_id` AggregateFunction(argMaxIf, String, Tuple(UInt8, DateTime64(9)), UInt8) COMMENT 'Rule that fired, e.g. pii.email_address.
@access: confidential',
  `source` AggregateFunction(argMaxIf, String, Tuple(UInt8, DateTime64(9)), UInt8) COMMENT 'Detection source, e.g. gitleaks or presidio.
@access: confidential',
  `risk_policy_id` AggregateFunction(argMaxIf, String, Tuple(UInt8, DateTime64(9)), UInt8) COMMENT 'Risk policy the message was scanned against.
@access: confidential',
  `chat_source` AggregateFunction(argMaxIf, String, Tuple(UInt8, DateTime64(9)), UInt8) COMMENT 'Product surface the scanned message came from.
@access: confidential',
  `team` AggregateFunction(argMaxIf, String, Tuple(UInt8, DateTime64(9)), UInt8) COMMENT 'Directory department of the resolved user at ingest.
@access: confidential-pii',
  `user_email` AggregateFunction(argMaxIf, String, Tuple(UInt8, DateTime64(9)), UInt8) COMMENT 'Email of the resolved internal user at ingest.
@access: confidential-pii',
  `external_user_id` AggregateFunction(argMaxIf, String, Tuple(UInt8, DateTime64(9)), UInt8) COMMENT 'Resolved external user id, for findings with no internal user.
@access: confidential-pii',
  `enforcement_outcome` AggregateFunction(argMaxIf, String, Tuple(UInt8, DateTime64(9)), UInt8) COMMENT 'What enforcement did with the finding, e.g. denied. Empty when nothing was enforced.
@access: confidential',
  `created_at_unix_nano` SimpleAggregateFunction(min, Int64) COMMENT 'Time the finding was produced. Every copy of a finding keeps it.
@access: confidential',
  INDEX `idx_risk_finding_states_created_at` ((created_at_unix_nano)) TYPE minmax
) ENGINE = AggregatingMergeTree
PRIMARY KEY (`organization_id`, `project_id`, `id`) ORDER BY (`organization_id`, `project_id`, `id`) TTL fromUnixTimestamp64Nano(created_at_unix_nano) + toIntervalDay(90) SETTINGS index_granularity = 8192 COMMENT 'One row per risk finding, folded from risk_findings by risk_finding_states_mv, for analytics counts.';
-- create "risk_finding_states_mv" view
CREATE MATERIALIZED VIEW `gram`.`risk_finding_states_mv` TO `gram`.`risk_finding_states` AS SELECT organization_id, project_id, id, argMaxState(toUInt8((excluded_at IS NULL) AND (false_positive_at IS NULL) AND (dead_letter_reason = '')), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at)) AS live, argMaxIfState(toString(category), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), category != '') AS category, argMaxIfState(toString(rule_id), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), rule_id != '') AS rule_id, argMaxIfState(toString(source), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), source != '') AS source, argMaxIfState(risk_policy_id, (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), risk_policy_id != '') AS risk_policy_id, argMaxIfState(toString(chat_source), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), chat_source != '') AS chat_source, argMaxIfState(toString(team), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), team != '') AS team, argMaxIfState(user_email, (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), user_email != '') AS user_email, argMaxIfState(external_user_id, (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), external_user_id != '') AS external_user_id, argMaxIfState(toString(enforcement_outcome), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), enforcement_outcome != '') AS enforcement_outcome, min(toUnixTimestamp64Nano(created_at)) AS created_at_unix_nano FROM gram.risk_findings WHERE shadow = 0 GROUP BY organization_id, project_id, id;
-- Backfill the findings already in risk_findings, which the view only folds
-- as they arrive. The view is created first, so no copy lands in the gap.
-- A copy both the view and this pass fold changes nothing, as argMax keeps
-- the same winning copy however often it sees it.
INSERT INTO `gram`.`risk_finding_states` SELECT organization_id, project_id, id, argMaxState(toUInt8((excluded_at IS NULL) AND (false_positive_at IS NULL) AND (dead_letter_reason = '')), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at)) AS live, argMaxIfState(toString(category), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), category != '') AS category, argMaxIfState(toString(rule_id), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), rule_id != '') AS rule_id, argMaxIfState(toString(source), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), source != '') AS source, argMaxIfState(risk_policy_id, (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), risk_policy_id != '') AS risk_policy_id, argMaxIfState(toString(chat_source), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), chat_source != '') AS chat_source, argMaxIfState(toString(team), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), team != '') AS team, argMaxIfState(user_email, (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), user_email != '') AS user_email, argMaxIfState(external_user_id, (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), external_user_id != '') AS external_user_id, argMaxIfState(toString(enforcement_outcome), (toUInt8((event_kind IN ('suppression', 'unsuppression'))), inserted_at), enforcement_outcome != '') AS enforcement_outcome, min(toUnixTimestamp64Nano(created_at)) AS created_at_unix_nano FROM gram.risk_findings WHERE shadow = 0 GROUP BY organization_id, project_id, id;
