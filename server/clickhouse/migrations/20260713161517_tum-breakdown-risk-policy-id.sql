-- Hand-edited (not atlas's DROP+recreate): add risk_policy_id as a sort-key
-- dimension to "tum_breakdown_summaries" NON-destructively, so the billing
-- page can break Risk Policy Analysis tokens down by the specific risk
-- policy whose scan produced them (attributes.gram.risk.policy_id, stamped
-- on judge completions by the scanners). ADD COLUMN and MODIFY ORDER BY must
-- be one ALTER statement — ClickHouse only lets MODIFY ORDER BY extend the
-- key with columns added in the same ALTER. Existing aggregates are
-- preserved (no DROP TABLE).
--
-- No backfill: the attribute was never emitted before the companion code
-- change, so pre-existing rows correctly keep risk_policy_id '' and read as
-- "(unset)". That also means no DELETE/re-derive passes here (contrast the
-- hook_source migration): the only write gap is the seconds between DROP
-- VIEW and CREATE VIEW below, a bounded UNDERCOUNT — the safe direction for
-- a billing record.
ALTER TABLE `tum_breakdown_summaries`
  ADD COLUMN `risk_policy_id` String,
  MODIFY ORDER BY (`gram_project_id`, `time_bucket`, `chat_id`, `hook_source`, `model`, `user_email`, `division_name`, `roles`, `risk_policy_id`);
-- Swap the MV to the new grouping key. Kept adjacent to minimize the
-- ingestion gap.
DROP VIEW `tum_breakdown_summaries_mv`;
CREATE MATERIALIZED VIEW `tum_breakdown_summaries_mv` TO `tum_breakdown_summaries` AS
SELECT
    gram_project_id,
    chat_id,
    toStartOfDay(fromUnixTimestamp64Nano(time_unix_nano, 'UTC')) AS time_bucket,
    hook_source,
    toString(attributes.gen_ai.response.model) AS model,
    user_email,
    toString(attributes.user.attributes.division_name) AS division_name,
    arraySort(JSONExtract(ifNull(toJSONString(attributes.user.roles), '[]'), 'Array(String)')) AS roles,
    toString(attributes.gram.risk.policy_id) AS risk_policy_id,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.input_tokens))) AS input_tokens,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.output_tokens))) AS output_tokens,
    sum(toInt64OrZero(toString(attributes.gen_ai.usage.total_tokens))) AS total_tokens
FROM telemetry_logs
WHERE chat_id != ''
  AND toString(attributes.gen_ai.usage.total_tokens) != ''
GROUP BY gram_project_id, chat_id, time_bucket, hook_source, model, user_email, division_name, roles, risk_policy_id;
