ALTER TABLE `gram`.`billing_meter_readings_by_time`
  ADD COLUMN `workload_source` LowCardinality(String) MATERIALIZED attributes['workload_source'] COMMENT 'Frozen reporting attribute for workload source.',
  ADD COLUMN `scan_execution_path` LowCardinality(String) MATERIALIZED attributes['scan_execution_path'] COMMENT 'Frozen reporting attribute for scan execution path.',
  ADD COLUMN `risk_policy_version` LowCardinality(String) MATERIALIZED attributes['risk_policy_version'] COMMENT 'Frozen reporting attribute for risk-policy version.',
  ADD COLUMN `risk_policy_link_status` LowCardinality(String) MATERIALIZED attributes['risk_policy_link_status'] COMMENT 'Frozen reporting attribute for risk-policy-link status.',
  ADD COLUMN `risk_policy_link_reason` LowCardinality(String) MATERIALIZED attributes['risk_policy_link_reason'] COMMENT 'Frozen reporting attribute for risk-policy-link reason.',
  ADD COLUMN `request_path` String MATERIALIZED attributes['request_path'] COMMENT 'Frozen reporting attribute for the request path.',
  ADD COLUMN `message_user_id` String MATERIALIZED attributes['message_user_id'] COMMENT 'Frozen reporting attribute for message user identity.',
  ADD COLUMN `message_type` LowCardinality(String) MATERIALIZED attributes['message_type'] COMMENT 'Frozen reporting attribute for message type.',
  ADD COLUMN `message_link_status` LowCardinality(String) MATERIALIZED attributes['message_link_status'] COMMENT 'Frozen reporting attribute for message-link status.',
  ADD COLUMN `message_link_reason` LowCardinality(String) MATERIALIZED attributes['message_link_reason'] COMMENT 'Frozen reporting attribute for message-link reason.',
  ADD COLUMN `hook_source` LowCardinality(String) MATERIALIZED attributes['hook_source'] COMMENT 'Frozen reporting attribute for the agent source.',
  ADD COLUMN `hook_hostname` String MATERIALIZED attributes['hook_hostname'] COMMENT 'Frozen reporting attribute for the agent device.',
  ADD COLUMN `account_type` LowCardinality(String) MATERIALIZED attributes['account_type'] COMMENT 'Frozen reporting attribute for account type.';
