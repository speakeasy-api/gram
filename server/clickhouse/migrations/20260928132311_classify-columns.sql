ALTER TABLE `gram`.`agent_events`
  COMMENT COLUMN `organization_id` 'Organization the record belongs to, from the provenance stamped at the ingest edge.
@access: confidential',
  COMMENT COLUMN `project_id` 'Project the record was ingested under, from the provenance stamped at the ingest edge.
@access: confidential',
  COMMENT COLUMN `occurred_at_unix_nano` 'Producer event time as Unix time (ns). What every query window filters on.
@access: confidential',
  COMMENT COLUMN `observed_at_unix_nano` 'Unix time (ns) when the record reached the ingest edge. Orders competing observations of the same fact.
@access: confidential',
  COMMENT COLUMN `record_id` 'One delivery key, resolved at ingest: the publisher record id for log-derived records, the span identity for span-derived ones. Readers de-duplicate on this and never branch on how a record arrived.
@access: confidential',
  COMMENT COLUMN `session_id` 'Agent session the record belongs to, as extracted by the dialect. Rows sharing a session_id are the same session by definition. Empty when the producer states none.
@access: confidential',
  COMMENT COLUMN `turn_id` 'Turn within the session, when the producer states one. Populated unevenly across producers.
@access: confidential',
  COMMENT COLUMN `event_id` 'Natural identity of the subject (the message, the tool call), shared across observations of it by design. A minted id when the producer states none.
@access: confidential',
  COMMENT COLUMN `event_type` 'Canonical event type assigned by the dialect. Says which kind of thing event_id names. Empty for records no dialect classified.
@access: confidential',
  COMMENT COLUMN `raw_event_name` 'The producer own name for the event. For a span-derived row this is the span name. Kept so an unclassified record stays reclassifiable.
@access: opaque-restricted',
  COMMENT COLUMN `source` 'Canonicalized producer surface derived from resource service.name at write time (e.g. claude-code, litellm). Empty when not stated.
@access: confidential',
  COMMENT COLUMN `provider` 'Model provider the record concerns (e.g. anthropic, openai). Empty when not stated.
@access: confidential',
  COMMENT COLUMN `surface` 'Agent surface the activity happened on (e.g. claude-code, codex, cursor). Empty when not stated.
@access: confidential',
  COMMENT COLUMN `user_id` 'Producer-stated user id. Empty when not stated.
@access: confidential-pii',
  COMMENT COLUMN `user_email` 'Producer-stated user email. Empty when not stated.
@access: confidential-pii',
  COMMENT COLUMN `external_user_id` 'User id in the provider own account system. Empty when not stated.
@access: confidential-pii',
  COMMENT COLUMN `account_type` 'Resolved account type. Empty until attribution runs in this pipeline.
@access: opaque-restricted',
  COMMENT COLUMN `billing_mode` 'Resolved billing mode. Empty until attribution runs in this pipeline.
@access: opaque-restricted',
  COMMENT COLUMN `external_org_id` 'Organization id in the provider own account system. Empty when not resolved.
@access: confidential-pii',
  COMMENT COLUMN `device_id` 'Device the activity came from, when the device agent reported one. Empty otherwise.
@access: confidential-pii',
  COMMENT COLUMN `department_name` 'Directory department of the user at ingest time. Empty when not enriched.
@access: confidential-pii',
  COMMENT COLUMN `division_name` 'Directory division of the user at ingest time. Empty when not enriched.
@access: confidential-pii',
  COMMENT COLUMN `job_title` 'Directory job title of the user at ingest time. Empty when not enriched.
@access: confidential-pii',
  COMMENT COLUMN `employee_type` 'Directory employee type of the user at ingest time. Empty when not enriched.
@access: confidential-pii',
  COMMENT COLUMN `cost_center_name` 'Directory cost center of the user at ingest time. Empty when not enriched.
@access: confidential-pii',
  COMMENT COLUMN `roles` 'Directory roles of the user at ingest time. Empty when not enriched.
@access: opaque-restricted',
  COMMENT COLUMN `groups` 'Directory groups of the user at ingest time. Empty when not enriched.
@access: opaque-restricted',
  COMMENT COLUMN `model` 'Model named by the record. Empty when not stated.
@access: confidential',
  COMMENT COLUMN `query_source` 'Where the request originated inside the agent (e.g. user_prompt, tool_result). Empty when not stated.
@access: confidential',
  COMMENT COLUMN `skill_name` 'Skill invoked, when the record says so. Empty otherwise.
@access: opaque-restricted',
  COMMENT COLUMN `agent_name` 'Sub-agent name, when the record says so. Empty otherwise.
@access: opaque-restricted',
  COMMENT COLUMN `mcp_server_name` 'MCP server involved, when the record says so. Empty otherwise.
@access: opaque-restricted',
  COMMENT COLUMN `mcp_tool_name` 'MCP tool involved, when the record says so. Empty otherwise.
@access: opaque-restricted',
  COMMENT COLUMN `tool_name` 'Tool the record concerns, when the record says so. Empty otherwise.
@access: confidential',
  COMMENT COLUMN `text` 'The record in words. Empty where the producer put everything in attributes, in which case raw_event_name is the readable headline.
@access: opaque-restricted',
  COMMENT COLUMN `outcome` 'Agent-vocabulary outcome: ok | error | rejected (a tool call the user or a policy refused to run) | refused (a model declining to answer) | empty when not stated. Not a protocol status code.
@access: confidential',
  COMMENT COLUMN `outcome_message` 'Producer-stated message accompanying an error outcome. Empty otherwise.
@access: opaque-restricted',
  COMMENT COLUMN `duration_nano` 'Duration in nanoseconds when the producer states one (span duration, tool call duration). 0 when not stated.
@access: confidential',
  COMMENT COLUMN `input_content` 'Normalized input message JSON. Empty when the record carries none.
@access: opaque-restricted',
  COMMENT COLUMN `output_content` 'Normalized output message JSON. Empty when the record carries none.
@access: opaque-restricted',
  COMMENT COLUMN `input_tokens` 'Input tokens stated on the record. 0 when not stated.
@access: confidential',
  COMMENT COLUMN `output_tokens` 'Output tokens stated on the record. 0 when not stated.
@access: confidential',
  COMMENT COLUMN `cache_read_tokens` 'Cache read tokens stated on the record. 0 when not stated.
@access: confidential',
  COMMENT COLUMN `cache_write_tokens` 'Cache write tokens stated on the record. 0 when not stated.
@access: confidential',
  COMMENT COLUMN `cost_usd` 'Cost in USD stated on the record. 0 when not stated.
@access: confidential',
  COMMENT COLUMN `attributes` 'Record attributes verbatim, including Gram enrichments. Anything not typed above survives here.
@access: opaque-restricted',
  COMMENT COLUMN `resource_attributes` 'Attributes of the resource that produced the record.
@access: opaque-restricted',
  COMMENT COLUMN `scope_attributes` 'Instrumentation scope attributes.
@access: opaque-restricted';
ALTER TABLE `gram`.`agent_metrics`
  COMMENT COLUMN `organization_id` 'Organization the data point belongs to, from the provenance stamped at the ingest edge.
@access: confidential',
  COMMENT COLUMN `project_id` 'Project the data point was ingested under, from the provenance stamped at the ingest edge.
@access: confidential',
  COMMENT COLUMN `occurred_at_unix_nano` 'The data point own time as Unix time (ns). What every query window filters on.
@access: confidential',
  COMMENT COLUMN `observed_at_unix_nano` 'Unix time (ns) when the data point reached the ingest edge. The ReplacingMergeTree version column.
@access: confidential',
  COMMENT COLUMN `window_start_unix_nano` 'Start of the interval the data point covers, as Unix time (ns). Equals occurred_at_unix_nano for point observations.
@access: confidential',
  COMMENT COLUMN `window_end_unix_nano` 'End of the interval the data point covers, as Unix time (ns). Equals occurred_at_unix_nano for point observations.
@access: confidential',
  COMMENT COLUMN `grain` 'Producer native reporting grain: turn | session | minute | hour | day | point. Gates rollup eligibility.
@access: confidential',
  COMMENT COLUMN `temporality` 'OTLP aggregation temporality: delta | cumulative | unspecified. Without it a reader cannot know whether sum() over a set of rows is valid.
@access: confidential',
  COMMENT COLUMN `is_monotonic` '1 when the producer declares the series monotonic, else 0.
@access: confidential',
  COMMENT COLUMN `metric_id` 'Content-derived fingerprint over the OTel identifying set (resource attributes, scope, metric name, unit, data point type, temporality, monotonicity, point attributes) plus the point timestamps. Value is excluded on purpose so a replay reproduces the same fingerprint.
@access: confidential',
  COMMENT COLUMN `metric_name` 'The producer own metric name, verbatim.
@access: confidential',
  COMMENT COLUMN `canonical_metric` 'The dialect canonical name for the measure. Discriminating point attributes fold in here, so claude_code.token.usage with type=input becomes its own canonical metric.
@access: confidential',
  COMMENT COLUMN `value` 'The measured value. One column rather than an int/double pair because Float64 is exact to 2^53 and the OTel spec says the numeric representation is not identifying.
@access: confidential',
  COMMENT COLUMN `unit` 'Unit as declared by the producer. Empty when not declared.
@access: confidential',
  COMMENT COLUMN `session_id` 'Agent session the data point is attributable to. Empty for feeds that report without one.
@access: confidential',
  COMMENT COLUMN `turn_id` 'Turn within the session, when the producer states one.
@access: confidential',
  COMMENT COLUMN `user_id` 'Producer-stated user id. Empty when not stated.
@access: confidential-pii',
  COMMENT COLUMN `user_email` 'Producer-stated user email. Empty when not stated.
@access: confidential-pii',
  COMMENT COLUMN `external_user_id` 'User id in the provider own account system. Empty when not stated.
@access: confidential-pii',
  COMMENT COLUMN `model` 'Model the measurement concerns. Empty when not stated.
@access: confidential',
  COMMENT COLUMN `query_source` 'Where the request originated inside the agent. Empty when not stated.
@access: confidential',
  COMMENT COLUMN `skill_name` 'Skill invoked, when stated. Empty otherwise.
@access: confidential',
  COMMENT COLUMN `agent_name` 'Sub-agent name, when stated. Empty otherwise.
@access: confidential',
  COMMENT COLUMN `mcp_server_name` 'MCP server involved, when stated. Empty otherwise.
@access: confidential',
  COMMENT COLUMN `mcp_tool_name` 'MCP tool involved, when stated. Empty otherwise.
@access: confidential',
  COMMENT COLUMN `source` 'Canonicalized producer surface derived from resource service.name at write time. Empty when not stated.
@access: confidential',
  COMMENT COLUMN `provider` 'Model provider the measurement concerns. Empty when not stated.
@access: confidential',
  COMMENT COLUMN `surface` 'Agent surface the activity happened on. Empty when not stated.
@access: confidential',
  COMMENT COLUMN `account_type` 'Resolved account type. Empty until attribution runs in this pipeline.
@access: confidential',
  COMMENT COLUMN `billing_mode` 'Resolved billing mode. Empty until attribution runs in this pipeline.
@access: confidential',
  COMMENT COLUMN `external_org_id` 'Organization id in the provider own account system. Empty when not resolved.
@access: confidential-pii',
  COMMENT COLUMN `device_id` 'Device the activity came from, when the device agent reported one. Empty otherwise.
@access: confidential-pii',
  COMMENT COLUMN `department_name` 'Directory department of the user at ingest time. Empty when not enriched.
@access: confidential-pii',
  COMMENT COLUMN `division_name` 'Directory division of the user at ingest time. Empty when not enriched.
@access: confidential-pii',
  COMMENT COLUMN `job_title` 'Directory job title of the user at ingest time. Empty when not enriched.
@access: confidential-pii',
  COMMENT COLUMN `employee_type` 'Directory employee type of the user at ingest time. Empty when not enriched.
@access: confidential-pii',
  COMMENT COLUMN `cost_center_name` 'Directory cost center of the user at ingest time. Empty when not enriched.
@access: confidential-pii',
  COMMENT COLUMN `roles` 'Directory roles of the user at ingest time. Empty when not enriched.
@access: opaque-restricted',
  COMMENT COLUMN `groups` 'Directory groups of the user at ingest time. Empty when not enriched.
@access: opaque-restricted',
  COMMENT COLUMN `attributes` 'Data point attributes verbatim, including Gram enrichments.
@access: opaque-restricted',
  COMMENT COLUMN `resource_attributes` 'Attributes of the resource that produced the data point.
@access: opaque-restricted',
  COMMENT COLUMN `scope_attributes` 'Instrumentation scope attributes.
@access: opaque-restricted';
ALTER TABLE `gram`.`ai_detections`
  COMMENT COLUMN `organization_id` 'Organization the reporting device agent is enrolled in.
@access: confidential',
  COMMENT COLUMN `target_id` 'Id of the detected AI tool as reported by the agent. Usually a server/internal/agent/aitargets catalog id, but stored as-is: agent binaries can ship newer target lists than the catalog knows.
@access: confidential',
  COMMENT COLUMN `device_serial` 'Normalized hardware serial of the reporting device. Empty when the agent cannot read one.
@access: confidential-pii',
  COMMENT COLUMN `user_email` 'Normalized email of the enrolled user the scan is attributed to.
@access: confidential-pii',
  COMMENT COLUMN `signal` 'Scan observation value validated by application code. Currently installed or running.
@access: confidential',
  COMMENT COLUMN `category` 'Detection category validated by application code. Currently harness or local_model. Known targets use the server catalog, while unknown ids keep a supported reported category.
@access: confidential',
  COMMENT COLUMN `version` 'Installed version when the scan could extract one statically. Empty otherwise.
@access: confidential',
  COMMENT COLUMN `first_seen` 'When this signal was first reported for this org, target, device, user. Preserved across upserts via read-merge-write.
@access: confidential',
  COMMENT COLUMN `last_seen` 'When this signal was most recently reported.
@access: confidential',
  COMMENT COLUMN `updated_at` 'Replacing-merge version column. The newest row per key wins.
@access: confidential';
ALTER TABLE `gram`.`ai_scan_receipts`
  COMMENT COLUMN `organization_id` 'Organization the reporting device agent is enrolled in.
@access: confidential',
  COMMENT COLUMN `device_serial` 'Normalized hardware serial of the reporting device. Empty when the agent cannot read one.
@access: confidential-pii',
  COMMENT COLUMN `user_email` 'Normalized email of the enrolled user the scan is attributed to.
@access: confidential-pii',
  COMMENT COLUMN `scan_started_at` 'When the agent started the scan, as reported.
@access: confidential',
  COMMENT COLUMN `scan_completed_at` 'When the agent completed the scan, as reported.
@access: confidential',
  COMMENT COLUMN `target_list_version` 'Version of the target list compiled into the agent binary that ran the scan, echoed as reported.
@access: confidential',
  COMMENT COLUMN `match_count` 'Number of matches reported by the agent. Zero-match scans still post a receipt so coverage is provable.
@access: confidential',
  COMMENT COLUMN `received_at` 'When the server received the report.
@access: confidential';
ALTER TABLE `gram`.`attribute_keys`
  COMMENT COLUMN `gram_project_id` '@access: confidential',
  COMMENT COLUMN `attribute_key` '@access: opaque-restricted',
  COMMENT COLUMN `first_seen_unix_nano` '@access: confidential',
  COMMENT COLUMN `last_seen_unix_nano` '@access: confidential';
ALTER TABLE `gram`.`attribute_metrics_summaries`
  COMMENT COLUMN `gram_project_id` '@access: confidential',
  COMMENT COLUMN `time_bucket` '@access: confidential',
  COMMENT COLUMN `department_name` '@access: confidential-pii',
  COMMENT COLUMN `job_title` '@access: confidential-pii',
  COMMENT COLUMN `employee_type` '@access: confidential-pii',
  COMMENT COLUMN `division_name` '@access: confidential-pii',
  COMMENT COLUMN `cost_center_name` '@access: confidential-pii',
  COMMENT COLUMN `user_email` '@access: confidential-pii',
  COMMENT COLUMN `model` '@access: confidential',
  COMMENT COLUMN `hook_source` '@access: confidential',
  COMMENT COLUMN `roles` '@access: confidential-pii',
  COMMENT COLUMN `groups` '@access: confidential-pii',
  COMMENT COLUMN `total_chats` '@access: confidential',
  COMMENT COLUMN `total_input_tokens` '@access: confidential',
  COMMENT COLUMN `total_output_tokens` '@access: confidential',
  COMMENT COLUMN `total_tokens` '@access: confidential',
  COMMENT COLUMN `cache_read_input_tokens` '@access: confidential',
  COMMENT COLUMN `cache_creation_input_tokens` '@access: confidential',
  COMMENT COLUMN `total_cost` '@access: confidential',
  COMMENT COLUMN `total_tool_calls` '@access: confidential',
  COMMENT COLUMN `unique_tool_calls` '@access: confidential',
  COMMENT COLUMN `account_type` '@access: confidential',
  COMMENT COLUMN `provider` '@access: confidential',
  COMMENT COLUMN `billing_mode` '@access: confidential',
  COMMENT COLUMN `query_source` '@access: confidential',
  COMMENT COLUMN `skill_name` '@access: confidential',
  COMMENT COLUMN `agent_name` '@access: confidential',
  COMMENT COLUMN `mcp_server_name` '@access: confidential',
  COMMENT COLUMN `mcp_tool_name` '@access: confidential',
  COMMENT COLUMN `is_active` '@access: confidential',
  COMMENT COLUMN `generation` '@access: confidential',
  COMMENT COLUMN `hook_hostname` '@access: confidential-pii',
  COMMENT COLUMN `total_work_units` '@access: confidential',
  COMMENT COLUMN `scored_cost` '@access: confidential',
  COMMENT COLUMN `scored_tokens` '@access: confidential';
ALTER TABLE `gram`.`authz_challenge_bucket_summaries`
  COMMENT COLUMN `challenge_date` 'UTC date of the summarized challenge rows.
@access: confidential',
  COMMENT COLUMN `organization_id` 'Organization the principal was acting in.
@access: confidential',
  COMMENT COLUMN `project_id` 'Project the check was scoped to.
@access: confidential',
  COMMENT COLUMN `outcome` 'Decision outcome.
@access: confidential',
  COMMENT COLUMN `scope` 'Scope of the focus check.
@access: confidential',
  COMMENT COLUMN `principal_urn` 'Primary principal URN.
@access: restricted',
  COMMENT COLUMN `user_id_filter` 'User ID used to suppress principals outside the organization.
@access: confidential',
  COMMENT COLUMN `resource_kind` 'Resource kind of the focus check.
@access: confidential',
  COMMENT COLUMN `resource_id` 'Resource ID of the focus check.
@access: confidential',
  COMMENT COLUMN `representative_id` '@access: confidential',
  COMMENT COLUMN `principal_type` '@access: confidential',
  COMMENT COLUMN `user_id` '@access: confidential',
  COMMENT COLUMN `user_email` '@access: confidential-pii',
  COMMENT COLUMN `operation` '@access: confidential',
  COMMENT COLUMN `reason` '@access: confidential',
  COMMENT COLUMN `role_slugs` '@access: confidential',
  COMMENT COLUMN `evaluated_grant_count` '@access: confidential',
  COMMENT COLUMN `matched_grant_count` '@access: confidential',
  COMMENT COLUMN `challenge_ids` '@access: confidential',
  COMMENT COLUMN `first_seen` '@access: confidential',
  COMMENT COLUMN `last_seen` '@access: confidential';
ALTER TABLE `gram`.`authz_challenges`
  COMMENT COLUMN `id` 'Unique identifier for the challenge entry.
@access: confidential',
  COMMENT COLUMN `timestamp` 'Time the authz decision was made.
@access: confidential',
  COMMENT COLUMN `organization_id` 'Organization the principal was acting in.
@access: confidential',
  COMMENT COLUMN `project_id` 'Project the check was scoped to. Empty string when the check is org-level (kept non-nullable so it can sit in the primary key).
@access: confidential',
  COMMENT COLUMN `trace_id` 'W3C trace ID linking the challenge to the originating request.
@access: confidential',
  COMMENT COLUMN `span_id` 'W3C span ID for the specific operation that triggered the challenge.
@access: confidential',
  COMMENT COLUMN `request_id` 'Internal request ID from RequestContext (when set).
@access: confidential',
  COMMENT COLUMN `principal_urn` 'Primary principal URN (e.g. user:<uuid> or api_key:<id>).
@access: restricted',
  COMMENT COLUMN `principal_type` 'Principal kind: user | api_key | assistant.
@access: confidential',
  COMMENT COLUMN `user_id` 'AuthContext.UserID at decision time.
@access: confidential',
  COMMENT COLUMN `user_external_id` 'Customer-supplied external user identifier.
@access: confidential-pii',
  COMMENT COLUMN `user_email` 'AuthContext.Email when present (session auth path).
@access: confidential-pii',
  COMMENT COLUMN `api_key_id` 'API key ID for api_key principals.
@access: confidential',
  COMMENT COLUMN `session_id` 'Session ID for session-authed principals.
@access: secret-restricted',
  COMMENT COLUMN `role_slugs` 'Role principal URNs whose grants were loaded for this principal.
@access: confidential',
  COMMENT COLUMN `operation` 'Authz operation: require | require_any | filter.
@access: confidential',
  COMMENT COLUMN `outcome` 'Decision outcome: allow | deny | error.
@access: confidential',
  COMMENT COLUMN `reason` 'Reason: grant_matched | no_grants | scope_unsatisfied | invalid_check | rbac_skipped_apikey | dev_override.
@access: confidential',
  COMMENT COLUMN `scope` 'Scope of the focus check.
@access: confidential',
  COMMENT COLUMN `resource_kind` 'Resource kind of the focus check.
@access: confidential',
  COMMENT COLUMN `resource_id` 'Resource ID of the focus check.
@access: confidential',
  COMMENT COLUMN `selector` 'JSON selector of the focus check.
@access: restricted',
  COMMENT COLUMN `expanded_scopes` 'Scope hierarchy expansion of the focus check.
@access: confidential',
  COMMENT COLUMN `requested_checks.scope` 'All checks requested in the call. length(requested_checks.scope) gives the count.
@access: restricted',
  COMMENT COLUMN `requested_checks.resource_kind` 'All checks requested in the call. length(requested_checks.scope) gives the count.
@access: restricted',
  COMMENT COLUMN `requested_checks.resource_id` 'All checks requested in the call. length(requested_checks.scope) gives the count.
@access: restricted',
  COMMENT COLUMN `requested_checks.selector` 'All checks requested in the call. length(requested_checks.scope) gives the count.
@access: restricted',
  COMMENT COLUMN `matched_grants.principal_urn` 'Grants that satisfied at least one requested check. Empty on deny.
@access: restricted',
  COMMENT COLUMN `matched_grants.scope` 'Grants that satisfied at least one requested check. Empty on deny.
@access: restricted',
  COMMENT COLUMN `matched_grants.selector` 'Grants that satisfied at least one requested check. Empty on deny.
@access: restricted',
  COMMENT COLUMN `matched_grants.matched_via_check_scope` 'Grants that satisfied at least one requested check. Empty on deny.
@access: restricted',
  COMMENT COLUMN `evaluated_grant_count` 'Total grants the principal had loaded on context at decision time.
@access: confidential',
  COMMENT COLUMN `filter_candidate_count` 'Number of resource IDs the Filter call considered.
@access: confidential',
  COMMENT COLUMN `filter_allowed_count` 'Number of resource IDs the Filter call returned as allowed.
@access: confidential';
ALTER TABLE `gram`.`billing_meter_daily_summaries`
  COMMENT COLUMN `organization_id` '@access: confidential',
  COMMENT COLUMN `family` '@access: confidential',
  COMMENT COLUMN `reading_kind` '@access: confidential',
  COMMENT COLUMN `facet` '@access: confidential',
  COMMENT COLUMN `day` '@access: confidential',
  COMMENT COLUMN `series_kind` '@access: confidential',
  COMMENT COLUMN `series_key` '@access: confidential-pii',
  COMMENT COLUMN `label` '@access: confidential-pii',
  COMMENT COLUMN `unit` '@access: confidential',
  COMMENT COLUMN `measurement_method` '@access: confidential',
  COMMENT COLUMN `quantity` '@access: confidential',
  COMMENT COLUMN `reading_count` '@access: confidential';
ALTER TABLE `gram`.`billing_meter_readings`
  COMMENT COLUMN `id` 'Deterministic reading UUID stable across redelivery.
@access: confidential',
  COMMENT COLUMN `organization_id` 'Organization that owns the workload.
@access: confidential',
  COMMENT COLUMN `project_id` 'Project that owns the workload.
@access: confidential',
  COMMENT COLUMN `meter_id` 'Registered workload meter identifier.
@access: confidential',
  COMMENT COLUMN `operation_id` 'Domain operation that produced the reading.
@access: confidential',
  COMMENT COLUMN `unit` 'Measurement unit.
@access: confidential',
  COMMENT COLUMN `value` 'Signed workload value where usage is positive and adjustments may be positive or negative.
@access: confidential',
  COMMENT COLUMN `occurred_at` 'Usage-effective UTC time when the metered work executed and the timestamp used for billing periods.
@access: confidential',
  COMMENT COLUMN `produced_at` 'UTC time when the producer created this reading variant and the ReplacingMergeTree version.
@access: confidential',
  COMMENT COLUMN `inserted_at` 'UTC time when ClickHouse received the row for delivery-lag diagnostics.
@access: confidential',
  COMMENT COLUMN `corrects_reading_id` 'Original reading corrected by this immutable adjustment.
@access: confidential',
  COMMENT COLUMN `reading_kind` 'Derived row kind based on whether the reading corrects an earlier reading.
@access: confidential',
  COMMENT COLUMN `attributes` 'Additional producer-supplied reading dimensions.
@access: opaque-restricted',
  COMMENT COLUMN `tokenizer_codec` 'Tokenizer codec promoted from attributes for billing analysis.
@access: confidential',
  COMMENT COLUMN `measurement_method` 'Rating-critical measurement implementation.
@access: confidential';
ALTER TABLE `gram`.`billing_meter_readings_by_time`
  COMMENT COLUMN `id` 'Deterministic reading UUID stable across redelivery.
@access: confidential',
  COMMENT COLUMN `organization_id` 'Organization that owns the workload.
@access: confidential',
  COMMENT COLUMN `project_id` 'Project that owns the workload.
@access: confidential',
  COMMENT COLUMN `meter_id` 'Registered workload meter identifier.
@access: confidential',
  COMMENT COLUMN `operation_id` 'Domain operation that produced the reading.
@access: confidential',
  COMMENT COLUMN `unit` 'Measurement unit.
@access: confidential',
  COMMENT COLUMN `measurement_method` 'Rating-critical measurement implementation.
@access: confidential',
  COMMENT COLUMN `value` 'Signed workload value where usage is positive and adjustments may be positive or negative.
@access: confidential',
  COMMENT COLUMN `occurred_at` 'Usage-effective UTC time when the metered work executed and the timestamp used for billing periods.
@access: confidential',
  COMMENT COLUMN `produced_at` 'UTC time when the producer created the accepted reading.
@access: confidential',
  COMMENT COLUMN `inserted_at` 'UTC time when ClickHouse received the row for delivery-lag diagnostics.
@access: confidential',
  COMMENT COLUMN `corrects_reading_id` 'Original reading corrected by this immutable adjustment.
@access: confidential',
  COMMENT COLUMN `reading_kind` 'Derived row kind based on whether the reading corrects an earlier reading.
@access: confidential',
  COMMENT COLUMN `attributes` 'Additional producer-supplied reading dimensions frozen at acceptance.
@access: opaque-restricted',
  COMMENT COLUMN `tokenizer_codec` 'Tokenizer codec promoted from attributes for billing analysis.
@access: confidential';
ALTER TABLE `gram`.`chat_analysis_scores`
  COMMENT COLUMN `id` 'Producer-supplied score identifier, the queue evaluation id.
@access: confidential',
  COMMENT COLUMN `created_at` 'Time the score was completed.
@access: confidential',
  COMMENT COLUMN `inserted_at` 'Time the score was inserted.
@access: confidential',
  COMMENT COLUMN `organization_id` 'Organization the score belongs to.
@access: confidential',
  COMMENT COLUMN `project_id` 'Project the score belongs to when known.
@access: confidential',
  COMMENT COLUMN `chat_id` 'Gram chat identifier of the analyzed session.
@access: confidential',
  COMMENT COLUMN `judge` 'Name of the analysis judge that produced the score.
@access: confidential',
  COMMENT COLUMN `score` 'Headline metric of the verdict, meaning defined per judge.
@access: confidential',
  COMMENT COLUMN `detail` 'Full structured verdict as JSON, shape defined per judge.
@access: opaque-restricted',
  COMMENT COLUMN `judge_model` 'Model used to judge the session.
@access: confidential',
  COMMENT COLUMN `judge_prompt_version` 'Judge prompt version.
@access: confidential';
ALTER TABLE `gram`.`chat_session_summaries`
  COMMENT COLUMN `gram_project_id` '@access: confidential',
  COMMENT COLUMN `time_bucket` '@access: confidential',
  COMMENT COLUMN `chat_id` '@access: confidential',
  COMMENT COLUMN `session_user_email` '@access: confidential-pii',
  COMMENT COLUMN `session_hook_source` '@access: confidential',
  COMMENT COLUMN `session_model` '@access: confidential',
  COMMENT COLUMN `start_time_unix_nano` '@access: confidential',
  COMMENT COLUMN `end_time_unix_nano` '@access: confidential',
  COMMENT COLUMN `message_count` '@access: confidential',
  COMMENT COLUMN `tool_call_count` '@access: confidential',
  COMMENT COLUMN `failed_tool_call_count` '@access: confidential',
  COMMENT COLUMN `total_input_tokens` '@access: confidential',
  COMMENT COLUMN `total_output_tokens` '@access: confidential',
  COMMENT COLUMN `total_tokens` '@access: confidential',
  COMMENT COLUMN `cache_read_input_tokens` '@access: confidential',
  COMMENT COLUMN `cache_creation_input_tokens` '@access: confidential',
  COMMENT COLUMN `total_cost` '@access: confidential',
  COMMENT COLUMN `department_names` '@access: confidential-pii',
  COMMENT COLUMN `job_titles` '@access: confidential-pii',
  COMMENT COLUMN `employee_types` '@access: confidential-pii',
  COMMENT COLUMN `division_names` '@access: confidential-pii',
  COMMENT COLUMN `cost_center_names` '@access: confidential-pii',
  COMMENT COLUMN `emails` '@access: confidential-pii',
  COMMENT COLUMN `hostnames` '@access: confidential-pii',
  COMMENT COLUMN `models` '@access: confidential',
  COMMENT COLUMN `hook_sources` '@access: confidential',
  COMMENT COLUMN `account_types` '@access: confidential',
  COMMENT COLUMN `providers` '@access: confidential',
  COMMENT COLUMN `billing_modes` '@access: confidential',
  COMMENT COLUMN `roles` '@access: confidential-pii',
  COMMENT COLUMN `groups` '@access: confidential-pii',
  COMMENT COLUMN `attribution_tuples` '@access: confidential';
ALTER TABLE `gram`.`chat_token_summaries`
  COMMENT COLUMN `gram_project_id` '@access: confidential',
  COMMENT COLUMN `chat_id` '@access: confidential',
  COMMENT COLUMN `time_bucket` '@access: confidential',
  COMMENT COLUMN `total_tokens` '@access: confidential',
  COMMENT COLUMN `stored_event_count` '@access: confidential',
  COMMENT COLUMN `hook_source` '@access: confidential';
ALTER TABLE `gram`.`identity_map`
  COMMENT COLUMN `org_id` 'Organization the identity belongs to.
@access: confidential',
  COMMENT COLUMN `email_lower` 'Normalized (lowercased, trimmed) email observed in telemetry.
@access: confidential-pii',
  COMMENT COLUMN `canonical_user_id` 'Directory user id the email resolves to.
@access: confidential',
  COMMENT COLUMN `canonical_email` 'Normalized directory email of the owning user - the fold target for analytics.
@access: confidential-pii';
ALTER TABLE `gram`.`identity_map_staging`
  COMMENT COLUMN `org_id` 'Organization the identity belongs to.
@access: confidential',
  COMMENT COLUMN `email_lower` 'Normalized (lowercased, trimmed) email observed in telemetry.
@access: confidential-pii',
  COMMENT COLUMN `canonical_user_id` 'Directory user id the email resolves to.
@access: confidential',
  COMMENT COLUMN `canonical_email` 'Normalized directory email of the owning user - the fold target for analytics.
@access: confidential-pii';
ALTER TABLE `gram`.`mcp_network_traffic_hourly_summaries`
  COMMENT COLUMN `gram_project_id` 'Project that received the inbound MCP request.
@access: confidential',
  COMMENT COLUMN `hour` 'UTC hour containing the observed request.
@access: confidential',
  COMMENT COLUMN `server_kind` 'MCP server kind: mcp or meta.
@access: confidential',
  COMMENT COLUMN `server_id` 'ID of the MCP server or meta MCP server.
@access: confidential',
  COMMENT COLUMN `surface` 'Network surface: public or private.
@access: confidential',
  COMMENT COLUMN `request_count` '@access: confidential',
  COMMENT COLUMN `last_seen` '@access: confidential';
ALTER TABLE `gram`.`metrics_summaries`
  COMMENT COLUMN `gram_project_id` '@access: confidential',
  COMMENT COLUMN `time_bucket` '@access: confidential',
  COMMENT COLUMN `first_seen_unix_nano` '@access: confidential',
  COMMENT COLUMN `last_seen_unix_nano` '@access: confidential',
  COMMENT COLUMN `total_chats` '@access: confidential',
  COMMENT COLUMN `distinct_models` '@access: confidential',
  COMMENT COLUMN `distinct_providers` '@access: confidential',
  COMMENT COLUMN `total_input_tokens` '@access: confidential',
  COMMENT COLUMN `total_output_tokens` '@access: confidential',
  COMMENT COLUMN `total_tokens` '@access: confidential',
  COMMENT COLUMN `avg_tokens_per_request` '@access: confidential',
  COMMENT COLUMN `total_chat_requests` '@access: confidential',
  COMMENT COLUMN `avg_chat_duration_ms` '@access: confidential',
  COMMENT COLUMN `finish_reason_stop` '@access: confidential',
  COMMENT COLUMN `finish_reason_tool_calls` '@access: confidential',
  COMMENT COLUMN `total_tool_calls` '@access: confidential',
  COMMENT COLUMN `tool_call_success` '@access: confidential',
  COMMENT COLUMN `tool_call_failure` '@access: confidential',
  COMMENT COLUMN `avg_tool_duration_ms` '@access: confidential',
  COMMENT COLUMN `chat_resolution_success` '@access: confidential',
  COMMENT COLUMN `chat_resolution_failure` '@access: confidential',
  COMMENT COLUMN `chat_resolution_partial` '@access: confidential',
  COMMENT COLUMN `chat_resolution_abandoned` '@access: confidential',
  COMMENT COLUMN `avg_chat_resolution_score` '@access: confidential',
  COMMENT COLUMN `evaluated_chats` '@access: confidential',
  COMMENT COLUMN `resolved_chats` '@access: confidential',
  COMMENT COLUMN `failed_chats` '@access: confidential',
  COMMENT COLUMN `avg_resolution_time_ms` '@access: confidential',
  COMMENT COLUMN `models` '@access: confidential',
  COMMENT COLUMN `tool_counts` '@access: confidential',
  COMMENT COLUMN `tool_success_counts` '@access: confidential',
  COMMENT COLUMN `tool_failure_counts` '@access: confidential',
  COMMENT COLUMN `cache_read_input_tokens` '@access: confidential',
  COMMENT COLUMN `cache_creation_input_tokens` '@access: confidential',
  COMMENT COLUMN `total_cost` '@access: confidential';
ALTER TABLE `gram`.`organization_metadata`
  COMMENT COLUMN `id` 'Gram organization identifier.
@access: confidential',
  COMMENT COLUMN `slug` 'Current Gram organization slug.
@access: confidential',
  COMMENT COLUMN `account_type` 'Gram account type, sourced from Postgres gram_account_type.
@access: confidential',
  COMMENT COLUMN `workos_id` 'WorkOS organization identifier used for cross-system reporting.
@access: confidential',
  COMMENT COLUMN `workos_updated_at` 'Timestamp of the latest applied WorkOS organization update.
@access: confidential',
  COMMENT COLUMN `webhooks_enabled` 'Whether outbound organization webhooks are enabled. Null means not recorded.
@access: confidential',
  COMMENT COLUMN `scim_enabled` 'Whether SCIM directory sync is enabled. Null means not recorded.
@access: confidential',
  COMMENT COLUMN `sso_enabled` 'Whether SSO is enabled. Null means not recorded.
@access: confidential',
  COMMENT COLUMN `whitelisted` 'Whether the organization is allowed to use Gram.
@access: confidential',
  COMMENT COLUMN `free_trial_started_at` 'Start of the organization metadata free-trial window.
@access: confidential',
  COMMENT COLUMN `free_trial_ends_at` 'End of the organization metadata free-trial window.
@access: confidential',
  COMMENT COLUMN `trial_tier` 'Enterprise trial tier from the trials lifecycle table.
@access: confidential',
  COMMENT COLUMN `trial_ends_at` 'Current enterprise trial end time.
@access: confidential',
  COMMENT COLUMN `trial_converted_at` 'When the enterprise trial converted.
@access: confidential',
  COMMENT COLUMN `trial_demoted_at` 'When the enterprise trial was demoted.
@access: confidential',
  COMMENT COLUMN `trial_created_at` 'When the enterprise trial lifecycle was created.
@access: confidential',
  COMMENT COLUMN `trial_updated_at` 'When the enterprise trial lifecycle was last updated.
@access: confidential',
  COMMENT COLUMN `created_at` 'When the organization was created in Gram.
@access: confidential',
  COMMENT COLUMN `updated_at` 'When the organization metadata was last updated in Gram.
@access: confidential',
  COMMENT COLUMN `disabled_at` 'When the organization was disabled.
@access: confidential';
ALTER TABLE `gram`.`organization_metadata_staging`
  COMMENT COLUMN `id` 'Gram organization identifier.
@access: confidential',
  COMMENT COLUMN `slug` 'Current Gram organization slug.
@access: confidential',
  COMMENT COLUMN `account_type` 'Gram account type, sourced from Postgres gram_account_type.
@access: confidential',
  COMMENT COLUMN `workos_id` 'WorkOS organization identifier used for cross-system reporting.
@access: confidential',
  COMMENT COLUMN `workos_updated_at` 'Timestamp of the latest applied WorkOS organization update.
@access: confidential',
  COMMENT COLUMN `webhooks_enabled` 'Whether outbound organization webhooks are enabled. Null means not recorded.
@access: confidential',
  COMMENT COLUMN `scim_enabled` 'Whether SCIM directory sync is enabled. Null means not recorded.
@access: confidential',
  COMMENT COLUMN `sso_enabled` 'Whether SSO is enabled. Null means not recorded.
@access: confidential',
  COMMENT COLUMN `whitelisted` 'Whether the organization is allowed to use Gram.
@access: confidential',
  COMMENT COLUMN `free_trial_started_at` 'Start of the organization metadata free-trial window.
@access: confidential',
  COMMENT COLUMN `free_trial_ends_at` 'End of the organization metadata free-trial window.
@access: confidential',
  COMMENT COLUMN `trial_tier` 'Enterprise trial tier from the trials lifecycle table.
@access: confidential',
  COMMENT COLUMN `trial_ends_at` 'Current enterprise trial end time.
@access: confidential',
  COMMENT COLUMN `trial_converted_at` 'When the enterprise trial converted.
@access: confidential',
  COMMENT COLUMN `trial_demoted_at` 'When the enterprise trial was demoted.
@access: confidential',
  COMMENT COLUMN `trial_created_at` 'When the enterprise trial lifecycle was created.
@access: confidential',
  COMMENT COLUMN `trial_updated_at` 'When the enterprise trial lifecycle was last updated.
@access: confidential',
  COMMENT COLUMN `created_at` 'When the organization was created in Gram.
@access: confidential',
  COMMENT COLUMN `updated_at` 'When the organization metadata was last updated in Gram.
@access: confidential',
  COMMENT COLUMN `disabled_at` 'When the organization was disabled.
@access: confidential';
ALTER TABLE `gram`.`otel_logs`
  COMMENT COLUMN `organization_id` 'Organization the record belongs to, from the provenance stamped at the ingest edge.
@access: confidential',
  COMMENT COLUMN `project_id` 'Project the record was ingested under, from the provenance stamped at the ingest edge.
@access: confidential',
  COMMENT COLUMN `time_unix_nano` 'Unix time (ns) when the event occurred. The writer substitutes observed_time_unix_nano when the producer sent 0 and drops records with neither, so this is never epoch zero.
@access: confidential',
  COMMENT COLUMN `observed_time_unix_nano` 'Unix time (ns) when the event was observed by the collection system. 0 when the producer omitted it.
@access: confidential',
  COMMENT COLUMN `timestamp` 'Human-readable timestamp derived from time_unix_nano.
@access: confidential',
  COMMENT COLUMN `source` 'Canonicalized producer surface derived from resource service.name at write time (e.g. claude-code, litellm). unknown when the resource carries no service.name.
@access: confidential',
  COMMENT COLUMN `trace_id` 'Hex-encoded W3C trace id (32 chars). Empty when the record has no span context.
@access: confidential',
  COMMENT COLUMN `span_id` 'Hex-encoded span id (16 chars). Empty when the record has no span context.
@access: confidential',
  COMMENT COLUMN `event_name` 'Event name for records that represent a named event (OTLP 1.5+). Empty otherwise.
@access: opaque-restricted',
  COMMENT COLUMN `severity_text` 'Producer-supplied severity text (DEBUG, INFO, WARN, ERROR, FATAL). Empty when unclassified.
@access: opaque-restricted',
  COMMENT COLUMN `severity_number` 'OTLP SeverityNumber enum value (1-24). 0 when unspecified.
@access: confidential',
  COMMENT COLUMN `body` 'Log body. String bodies are stored verbatim and structured bodies are JSON-encoded.
@access: opaque-restricted',
  COMMENT COLUMN `log_attributes` 'Log record attributes, including Gram enrichments applied by the transform pipeline.
@access: opaque-restricted',
  COMMENT COLUMN `flags` 'W3C trace flags for the emitting span context.
@access: confidential',
  COMMENT COLUMN `resource_attributes` 'Attributes of the resource that produced the record.
@access: opaque-restricted',
  COMMENT COLUMN `resource_schema_url` 'Schema URL of the resource. Empty when not reported.
@access: opaque-restricted',
  COMMENT COLUMN `scope_name` 'Instrumentation scope name. The transform pipeline rewrites this to com.speakeasy.ai.logging and keeps the producer scope in log_attributes under speakeasy.original_instrumentation_scope.name.
@access: confidential',
  COMMENT COLUMN `scope_version` 'Instrumentation scope version. Empty when not reported.
@access: confidential',
  COMMENT COLUMN `scope_attributes` 'Instrumentation scope attributes.
@access: opaque-restricted';
ALTER TABLE `gram`.`otel_traces`
  COMMENT COLUMN `organization_id` 'Organization the span belongs to, from the provenance stamped at the ingest edge.
@access: confidential',
  COMMENT COLUMN `project_id` 'Project the span was ingested under, from the provenance stamped at the ingest edge.
@access: confidential',
  COMMENT COLUMN `time_unix_nano` 'Span start time as Unix time (ns). Validated non-zero at the ingest edge.
@access: confidential',
  COMMENT COLUMN `timestamp` 'Human-readable timestamp derived from time_unix_nano.
@access: confidential',
  COMMENT COLUMN `duration_nano` 'Span duration in nanoseconds (end time minus start time).
@access: confidential',
  COMMENT COLUMN `source` 'Canonicalized producer surface derived from resource service.name at write time (e.g. claude-code, litellm). unknown when the resource carries no service.name.
@access: confidential',
  COMMENT COLUMN `trace_id` 'Hex-encoded W3C trace id (32 chars).
@access: confidential',
  COMMENT COLUMN `span_id` 'Hex-encoded span id (16 chars).
@access: confidential',
  COMMENT COLUMN `parent_span_id` 'Hex-encoded parent span id. Empty for root spans.
@access: confidential',
  COMMENT COLUMN `span_name` 'Span name.
@access: opaque-restricted',
  COMMENT COLUMN `span_kind` 'OTLP span kind: unspecified | internal | server | client | producer | consumer.
@access: confidential',
  COMMENT COLUMN `status_code` 'OTLP status code: unspecified | ok | error.
@access: confidential',
  COMMENT COLUMN `status_message` 'Status message, set only when status_code is error.
@access: opaque-restricted',
  COMMENT COLUMN `trace_state` 'W3C tracestate header value. Empty when not reported.
@access: opaque-restricted',
  COMMENT COLUMN `span_attributes` 'Span attributes, including Gram enrichments applied by the transform pipeline.
@access: opaque-restricted',
  COMMENT COLUMN `resource_attributes` 'Attributes of the resource that produced the span.
@access: opaque-restricted',
  COMMENT COLUMN `resource_schema_url` 'Schema URL of the resource. Empty when not reported.
@access: opaque-restricted',
  COMMENT COLUMN `scope_name` 'Instrumentation scope name. The transform pipeline rewrites this to com.speakeasy.ai.logging and keeps the producer scope in span_attributes under speakeasy.original_instrumentation_scope.name.
@access: confidential',
  COMMENT COLUMN `scope_version` 'Instrumentation scope version. Empty when not reported.
@access: confidential',
  COMMENT COLUMN `scope_attributes` 'Instrumentation scope attributes.
@access: opaque-restricted';
ALTER TABLE `gram`.`projects`
  COMMENT COLUMN `id` 'Gram project identifier.
@access: confidential',
  COMMENT COLUMN `organization_id` 'Organization that owns the project.
@access: confidential',
  COMMENT COLUMN `slug` 'Current project slug within its organization.
@access: confidential',
  COMMENT COLUMN `created_at` 'When the project was created.
@access: confidential',
  COMMENT COLUMN `updated_at` 'When the project was last updated.
@access: confidential',
  COMMENT COLUMN `deleted_at` 'When the project was soft-deleted.
@access: confidential';
ALTER TABLE `gram`.`projects_staging`
  COMMENT COLUMN `id` 'Gram project identifier.
@access: confidential',
  COMMENT COLUMN `organization_id` 'Organization that owns the project.
@access: confidential',
  COMMENT COLUMN `slug` 'Current project slug within its organization.
@access: confidential',
  COMMENT COLUMN `created_at` 'When the project was created.
@access: confidential',
  COMMENT COLUMN `updated_at` 'When the project was last updated.
@access: confidential',
  COMMENT COLUMN `deleted_at` 'When the project was soft-deleted.
@access: confidential';
ALTER TABLE `gram`.`risk_findings`
  COMMENT COLUMN `id` 'Finding UUIDv7, supplied by the scanner that produced it.
@access: confidential',
  COMMENT COLUMN `created_at` 'Time the finding was produced.
@access: confidential',
  COMMENT COLUMN `organization_id` 'Organization the finding belongs to (HKDF salt for the tenant fingerprint).
@access: confidential',
  COMMENT COLUMN `project_id` 'Project the finding was scoped to. Empty string when unknown, kept non-nullable so it can sit in the primary key.
@access: confidential',
  COMMENT COLUMN `request_id` 'Internal request ID that produced the finding, when set.
@access: confidential',
  COMMENT COLUMN `chat_message_id` 'Chat message the finding was detected in.
@access: confidential',
  COMMENT COLUMN `risk_policy_id` 'Risk policy the message was scanned against.
@access: confidential',
  COMMENT COLUMN `risk_policy_version` 'Version of the risk policy at scan time.
@access: confidential',
  COMMENT COLUMN `rule_id` 'Rule that fired, e.g. pii.email_address.
@access: confidential',
  COMMENT COLUMN `description` 'Human-readable description of the rule that fired.
@access: opaque-restricted',
  COMMENT COLUMN `source` 'Detection source: gitleaks | presidio | shadow_mcp | prompt_injection | llm_judge | account_identity.
@access: confidential',
  COMMENT COLUMN `confidence` 'Detection confidence in the range 0.0 to 1.0.
@access: confidential',
  COMMENT COLUMN `tags` 'Category tags for the finding, e.g. [pii].
@access: confidential',
  COMMENT COLUMN `start_pos` 'Byte offset of the match start within the scanned field.
@access: confidential',
  COMMENT COLUMN `end_pos` 'Byte offset of the match end within the scanned field.
@access: confidential',
  COMMENT COLUMN `dead_letter_reason` 'Non-empty marks a synthetic could-not-analyze sentinel rather than a real finding.
@access: opaque-restricted',
  COMMENT COLUMN `match_len` 'Byte length of the raw match, used to render the redacted display.
@access: confidential',
  COMMENT COLUMN `match_redacted` 'Partial-mask display string rendered as the match by default in listings. General tier shows first 4 and last 2 characters for length 8 and up, first 2 and last 1 for length 5 to 7, first 1 and last 1 below 5, stars in between. Financial-category matches show only the last 4 characters. Emails show ***@ followed by the real domain. Prompt injection, llm_judge and destructive sources leave it empty since the rationale carries the signal. Shadow MCP stores the server identifier verbatim as a documented carve-out. Storing boundary characters of real matches here is a deliberate, signed-off relaxation of the earlier no-plaintext rule per the reveal-from-ClickHouse design. Rows written before the change may still carry the legacy redacted len=N sha=XXXX form.
@access: opaque-restricted',
  COMMENT COLUMN `fingerprint_pepper_version` 'Pepper keyring version used to compute the fingerprints.
@access: confidential',
  COMMENT COLUMN `fingerprint_global_hs256` 'Global fingerprint: base64url HMAC-SHA256 of the match under the current pepper. Stable across tenants.
@access: secret-restricted',
  COMMENT COLUMN `fingerprint_tenant_hs256` 'Tenant-qualified fingerprint: base64url HMAC-SHA256 under a per-org HKDF key. Used to dedupe unique matches within an org.
@access: secret-restricted',
  COMMENT COLUMN `inserted_at` 'Server-side ingestion time, stamped on insert. Used for ingestion-lag diagnostics and to tie-break redelivered rows sharing the same id.
@access: confidential',
  COMMENT COLUMN `excluded_at` 'Time the finding was suppressed by an exclusion. Null when the finding is not excluded.
@access: confidential',
  COMMENT COLUMN `exclusion_id` 'Id of the risk_exclusions row that suppressed the finding. Null when the finding is not excluded.
@access: confidential',
  COMMENT COLUMN `chat_id` 'Denormalized chats.id of the chat the finding was detected in. Empty when unresolved.
@access: confidential',
  COMMENT COLUMN `user_id` 'Resolved internal user id: chat_messages.user_id with fallback to chats.user_id. Empty when unresolved.
@access: confidential',
  COMMENT COLUMN `external_user_id` 'Resolved external user id: chat_messages.external_user_id with fallback to chats.external_user_id. Empty when unresolved.
@access: confidential-pii',
  COMMENT COLUMN `category` 'Risk category derived from rule_id and source at ingest, e.g. pii or secrets. Empty when the rule maps to no category.
@access: confidential',
  COMMENT COLUMN `false_positive_at` 'Time the finding was marked a false positive, mirrored from Postgres after the fact. Null when the finding is not marked.
@access: confidential',
  COMMENT COLUMN `content_part_id` 'Chat content part the finding was detected in.
@access: confidential',
  COMMENT COLUMN `message_created_at` 'Event time of the scanned chat message (chat_messages.created_at). Defaults to created_at (scan time) for rows written before the column existed or when attribution is unresolved.
@access: confidential',
  COMMENT COLUMN `assistant_id` 'Assistant linked to the finding chat via a live assistant_threads row at ingest. Empty when the chat has no assistant link or attribution is unresolved.
@access: confidential',
  COMMENT COLUMN `surface` 'Which text start_pos and end_pos index: content, scan_surface, tool_args, json_path, derived, legacy_presidio or none. Empty for rows written before this column existed, where reveal falls back to a verified candidate cascade.
@access: confidential',
  COMMENT COLUMN `field` 'Scanner field the finding was detected in, e.g. content or tool.args, copied from the scanner span. Empty when unknown.
@access: confidential',
  COMMENT COLUMN `path` 'JSON path of the extracted value within the field for json_path surfaces, gjson syntax. Empty otherwise.
@access: opaque-restricted',
  COMMENT COLUMN `tool_call_id` 'Recorded tool call id anchoring the finding when the scanned text belongs to a tool call. Empty when not applicable or unknown.
@access: confidential',
  COMMENT COLUMN `chat_source` 'Canonical product surface the scanned message came from (chat_messages.source canonicalized at ingest, e.g. codex, cursor, claude-code). Empty for rows written before the column existed or when attribution is unresolved.
@access: confidential',
  COMMENT COLUMN `team` 'WorkOS directory department_name of the resolved user at ingest. Empty when the user has no directory profile or attribution is unresolved.
@access: confidential-pii',
  COMMENT COLUMN `user_email` 'Email of the resolved internal user at ingest (users.email), letting the Watchdog display users without a Postgres lookup. Empty for external-only users or when attribution is unresolved.
@access: confidential-pii',
  COMMENT COLUMN `excluded_reason` 'Why the finding was suppressed: rule (exclusion rule, exclusion_id set), manual (dismissed by a user via UI or agent tool) or automated (offline false-positive sweep). Empty when the finding is not suppressed or on legacy rows written before this column existed.
@access: confidential',
  COMMENT COLUMN `excluded_detail` 'Free-form context for the suppression: the user-supplied dismissal reason for manual rows, the false-positive catalog reason for automated rows. Empty for rule rows and when no reason was given.
@access: opaque-restricted',
  COMMENT COLUMN `event_kind` 'Kind of this copy of the finding: finding (scanner output, dead-letter sentinels included), suppression or unsuppression (appended state-change copies from manual dismiss/undo and the retroactive exclusion reconcile). Empty on rows written before this column existed - such rows rank as finding copies.
@access: confidential',
  COMMENT COLUMN `execution_id` 'Correlation ID of one mediated execution. Empty for legacy and chat-only findings.
@access: confidential',
  COMMENT COLUMN `mcp_server_id` 'Canonical concrete MCP server ID. Empty when unavailable.
@access: confidential',
  COMMENT COLUMN `meta_mcp_server_id` 'Outer meta MCP gateway ID, separate from the concrete member server.
@access: confidential',
  COMMENT COLUMN `toolset_id` 'Persisted toolset ID, empty for runtime-only or unavailable toolsets.
@access: confidential',
  COMMENT COLUMN `tool_name` 'Resolved concrete tool name, empty when unavailable.
@access: confidential',
  COMMENT COLUMN `phase` 'Inspection phase: request or response. Empty for legacy findings.
@access: confidential',
  COMMENT COLUMN `mediation_surface` 'Concrete mediation seam producing the finding.
@access: confidential',
  COMMENT COLUMN `mcp_method` 'MCP method or equivalent mediated operation, such as tools/call.
@access: confidential',
  COMMENT COLUMN `principal_kind` 'Credential class established exclusively by mcpidentity. Empty when unstamped.
@access: confidential',
  COMMENT COLUMN `identity_stamped` 'Whether validated principal provenance was present, including validated anonymous sessions.
@access: confidential',
  COMMENT COLUMN `enforcement_outcome` 'Enforcement action taken, independent of detection. Empty when unspecified or legacy.
@access: confidential',
  COMMENT COLUMN `shadow` 'Set to 1 on findings the LLM analyzer produced while the organization ran the shadow risk engine mode, recorded to compare the model with the legacy engines per message and never enforced. Every user-facing read path filters shadow = 0, so shadow rows are hidden from Risk Events, the overview, signals, the Watchdog and reveal by default. 0 on every enforcing finding and on rows written before this column existed.
@access: confidential';
ALTER TABLE `gram`.`shadow_mcp_inventory_urls`
  COMMENT COLUMN `gram_project_id` '@access: confidential',
  COMMENT COLUMN `canonical_server_url` '@access: opaque-restricted',
  COMMENT COLUMN `url_host` '@access: restricted',
  COMMENT COLUMN `server_name` '@access: confidential',
  COMMENT COLUMN `server_name_override` '@access: confidential',
  COMMENT COLUMN `first_seen` '@access: confidential',
  COMMENT COLUMN `last_seen` '@access: confidential',
  COMMENT COLUMN `updated_at` '@access: confidential';
ALTER TABLE `gram`.`skill_efficacy_scores`
  COMMENT COLUMN `id` 'Producer-supplied score identifier.
@access: confidential',
  COMMENT COLUMN `created_at` 'Time the score was completed.
@access: confidential',
  COMMENT COLUMN `inserted_at` 'Time the score was inserted.
@access: confidential',
  COMMENT COLUMN `organization_id` 'Organization the score belongs to.
@access: confidential',
  COMMENT COLUMN `project_id` 'Project the score belongs to when known.
@access: confidential',
  COMMENT COLUMN `session_id` 'Scoring session identifier, using the hook session ID for dev and chat ID for assistant.
@access: confidential',
  COMMENT COLUMN `skill_id` 'Evaluated skill identifier.
@access: confidential',
  COMMENT COLUMN `skill_version_id` 'Evaluated skill version identifier.
@access: confidential',
  COMMENT COLUMN `canonical_sha256` 'Canonical SHA-256 digest of the evaluated skill.
@access: confidential',
  COMMENT COLUMN `surface` 'Evaluation surface: dev | assistant.
@access: confidential',
  COMMENT COLUMN `trace_id` 'W3C trace ID for the evaluated session when available.
@access: confidential',
  COMMENT COLUMN `gram_chat_id` 'Gram chat identifier when available.
@access: confidential',
  COMMENT COLUMN `score` 'Skill efficacy score in the range 0.0 to 1.0.
@access: confidential',
  COMMENT COLUMN `rationale` 'Judge rationale for the score, capped at 200 characters.
@access: opaque-restricted',
  COMMENT COLUMN `est_turns_saved` 'Estimated conversation turns saved.
@access: confidential',
  COMMENT COLUMN `est_minutes_saved` 'Estimated minutes saved.
@access: confidential',
  COMMENT COLUMN `roi_confidence` 'ROI estimate confidence when estimated: low | med | high.
@access: confidential',
  COMMENT COLUMN `flags` 'Assessment flags: ignored | misapplied | partially_followed | harmful.
@access: confidential',
  COMMENT COLUMN `judge_model` 'Model used to judge efficacy.
@access: confidential',
  COMMENT COLUMN `judge_prompt_version` 'Judge prompt version.
@access: confidential';
ALTER TABLE `gram`.`skill_session_versions`
  COMMENT COLUMN `id` 'Source observation identifier.
@access: confidential',
  COMMENT COLUMN `created_at` 'Time the mapping was created.
@access: confidential',
  COMMENT COLUMN `inserted_at` 'Time the mapping was inserted.
@access: confidential',
  COMMENT COLUMN `seen_at` 'Time the skill version became active in the session.
@access: confidential',
  COMMENT COLUMN `organization_id` 'Organization the session belongs to.
@access: confidential',
  COMMENT COLUMN `project_id` 'Project the session belongs to.
@access: confidential',
  COMMENT COLUMN `session_id` 'Session identifier.
@access: confidential',
  COMMENT COLUMN `skill_id` 'Observed skill identifier.
@access: confidential',
  COMMENT COLUMN `skill_version_id` 'Observed skill version identifier.
@access: confidential',
  COMMENT COLUMN `canonical_sha256` 'Canonical SHA-256 digest of the observed skill.
@access: confidential',
  COMMENT COLUMN `surface` 'Observation surface: dev | assistant.
@access: confidential';
ALTER TABLE `gram`.`spend_rule_usage_summaries`
  COMMENT COLUMN `gram_project_id` '@access: confidential',
  COMMENT COLUMN `user_email` '@access: confidential-pii',
  COMMENT COLUMN `time_bucket` '@access: confidential',
  COMMENT COLUMN `total_cost` '@access: confidential';
ALTER TABLE `gram`.`telemetry_logs`
  COMMENT COLUMN `id` 'Unique identifier for the log entry.
@access: confidential',
  COMMENT COLUMN `time_unix_nano` 'Unix time (ns) when the event occurred measured by the origin clock.
@access: confidential',
  COMMENT COLUMN `observed_time_unix_nano` 'Unix time (ns) when the event was observed by the collection system.
@access: confidential',
  COMMENT COLUMN `severity_text` 'Text representation of severity (DEBUG, INFO, WARN, ERROR, FATAL).
@access: opaque-restricted',
  COMMENT COLUMN `body` 'The primary log message extracted from the log record. For structured logs, this is the human-readable message component.
@access: opaque-restricted',
  COMMENT COLUMN `trace_id` 'W3C trace ID linking related logs across services.
@access: confidential',
  COMMENT COLUMN `span_id` 'W3C span ID for specific operation within a trace.
@access: confidential',
  COMMENT COLUMN `attributes` 'Additional attributes about the specific event occurrence.
@access: opaque-restricted',
  COMMENT COLUMN `resource_attributes` 'Attributes describing the resource that generated this log.
@access: opaque-restricted',
  COMMENT COLUMN `gram_project_id` 'Project ID (denormalized from resource_attributes).
@access: confidential',
  COMMENT COLUMN `gram_deployment_id` 'Deployment ID (denormalized from resource_attributes).
@access: confidential',
  COMMENT COLUMN `gram_function_id` 'Function ID that generated the log (null for HTTP logs).
@access: confidential',
  COMMENT COLUMN `gram_urn` 'The Gram URN (e.g. tools:function:my-source:my-tool).
@access: confidential',
  COMMENT COLUMN `service_name` 'Logical service name (e.g., gram-functions, gram-http-gateway).
@access: confidential',
  COMMENT COLUMN `service_version` 'Service version.
@access: confidential',
  COMMENT COLUMN `observed_timestamp` 'Human-readable timestamp derived from observed_time_unix_nano.
@access: confidential',
  COMMENT COLUMN `gram_chat_id` 'The Chat ID associated with the log (if generated by chat).
@access: confidential',
  COMMENT COLUMN `project_id` 'Project ID (materialized from attributes.gram.project.id).
@access: confidential',
  COMMENT COLUMN `deployment_id` 'Deployment ID (materialized from resource_attributes.gram.deployment.id).
@access: confidential',
  COMMENT COLUMN `function_id` 'Function ID (materialized from attributes.gram.function.id).
@access: confidential',
  COMMENT COLUMN `urn` 'Tool URN (materialized from attributes.gram.tool.urn).
@access: confidential',
  COMMENT COLUMN `chat_id` 'Chat ID (materialized from attributes.gen_ai.conversation.id).
@access: confidential',
  COMMENT COLUMN `user_id` 'User ID (materialized from attributes.user.id).
@access: confidential-pii',
  COMMENT COLUMN `external_user_id` 'External user ID (materialized from attributes.gram.external_user.id).
@access: confidential-pii',
  COMMENT COLUMN `api_key_id` 'API key ID (materialized from attributes.gram.api_key.id).
@access: confidential',
  COMMENT COLUMN `evaluation_score_label` 'Evaluation result label (success, failure, partial, abandoned).
@access: confidential',
  COMMENT COLUMN `tool_name` 'Tool name (materialized from attributes.gram.tool.name).
@access: confidential',
  COMMENT COLUMN `tool_source` 'Tool call source (materialized from attributes.gram.tool_call.source).
@access: confidential',
  COMMENT COLUMN `event_source` 'Event source (materialized from attributes.gram.event.source).
@access: confidential',
  COMMENT COLUMN `toolset_slug` 'Toolset slug (materialized from attributes.gram.toolset.slug).
@access: confidential',
  COMMENT COLUMN `user_email` 'User email (materialized from attributes.user.email).
@access: confidential-pii',
  COMMENT COLUMN `hook_source` 'Hook source (materialized from attributes.gram.hook.source).
@access: confidential',
  COMMENT COLUMN `skill_name` 'Skill name extracted from tool arguments when tool_name is Skill (materialized).
@access: opaque-restricted',
  COMMENT COLUMN `hook_block_reason` 'Hook block reason set when the Gram hook denied a tool call (materialized from attributes.gram.hook.block_reason).
@access: opaque-restricted',
  COMMENT COLUMN `remote_mcp_server_id` 'Remote MCP server ID (materialized from attributes.gram.remote_mcp_server.id).
@access: confidential',
  COMMENT COLUMN `mcp_server_id` 'MCP server ID (materialized from attributes.gram.mcp_server.id).
@access: confidential',
  COMMENT COLUMN `provider` 'AI provider for the session account (e.g. anthropic, openai). Set by ingest (materialized from attributes.gram.provider).
@access: confidential',
  COMMENT COLUMN `external_org_id` 'Provider organization id for the account the user was logged into on-device (e.g. Claude organization.id). Distinct from the Gram org. Personal-account tracking discriminator. Normalized by ingest (materialized from attributes.gram.external_org_id).
@access: confidential-pii',
  COMMENT COLUMN `account_type` 'team (company/enterprise account) or personal (individual account). Set by ingest. Empty until classified (materialized from attributes.gram.account_type).
@access: confidential',
  COMMENT COLUMN `billing_mode` 'How the account is billed: metered (pay-per-token, cost is real spend) | flat_rate (subscription seat, cost is an estimate) | unknown | empty. Resolved by ingest from admin-declared config (materialized from attributes.gram.billing_mode).
@access: confidential',
  COMMENT COLUMN `event_urn` 'Canonical event identity in the form urn:telemetry:<origin>:<kind>:<type> where origin is the observation channel (provider_otel | provider_api | agent_hook | gram_service | unknown), kind is the signal shape (log | metric | span) and type is the producer event type lowercased. Stamped by telemetry.Logger. Empty on rows written before the column existed (materialized from attributes.gram.event.urn).
@access: opaque-restricted',
  COMMENT COLUMN `meta_mcp_server_id` 'Meta MCP server (Gateway Endpoint) ID when the call was dispatched through a gateway (materialized from attributes.gram.meta_mcp_server.id).
@access: confidential',
  COMMENT COLUMN `mcp_client_name` 'MCP client name self-reported at the initialize handshake or in the per-request _meta hint. Untrusted, attribution only. Empty for hook-observed and non-MCP traffic (materialized from attributes.gram.mcp.client.name).
@access: confidential',
  COMMENT COLUMN `mcp_client_version` 'MCP client version reported alongside mcp_client_name. Empty when the client reported a name but no version (materialized from attributes.gram.mcp.client.version).
@access: confidential';
ALTER TABLE `gram`.`telemetry_logs_staging`
  COMMENT COLUMN `id` 'Unique identifier for the log entry, preserved when the row is promoted to telemetry_logs.
@access: confidential',
  COMMENT COLUMN `time_unix_nano` 'Unix time (ns) when the event occurred measured by the origin clock.
@access: confidential',
  COMMENT COLUMN `observed_time_unix_nano` 'Unix time (ns) when the event was observed by the collection system.
@access: confidential',
  COMMENT COLUMN `observed_timestamp` 'Human-readable timestamp derived from observed_time_unix_nano.
@access: confidential',
  COMMENT COLUMN `severity_text` 'Text representation of severity (DEBUG, INFO, WARN, ERROR, FATAL).
@access: opaque-restricted',
  COMMENT COLUMN `body` 'The primary log message extracted from the log record.
@access: opaque-restricted',
  COMMENT COLUMN `trace_id` 'W3C trace ID linking related logs across services.
@access: confidential',
  COMMENT COLUMN `span_id` 'W3C span ID for specific operation within a trace.
@access: confidential',
  COMMENT COLUMN `attributes` 'Additional attributes about the specific event occurrence.
@access: opaque-restricted',
  COMMENT COLUMN `resource_attributes` 'Attributes describing the resource that generated this log.
@access: opaque-restricted',
  COMMENT COLUMN `gram_project_id` 'Project ID (denormalized from resource_attributes).
@access: confidential',
  COMMENT COLUMN `gram_deployment_id` 'Deployment ID (denormalized from resource_attributes).
@access: confidential',
  COMMENT COLUMN `gram_function_id` 'Function ID that generated the log (null for HTTP logs).
@access: confidential',
  COMMENT COLUMN `gram_urn` 'The Gram URN (e.g. claude-code:otel:logs).
@access: confidential',
  COMMENT COLUMN `service_name` 'Logical service name.
@access: confidential',
  COMMENT COLUMN `service_version` 'Service version.
@access: confidential',
  COMMENT COLUMN `gram_chat_id` 'The Chat ID (Claude session id) associated with the log.
@access: confidential',
  COMMENT COLUMN `chat_id` 'Chat ID (materialized from attributes.gen_ai.conversation.id) — the promotion worker scopes by this.
@access: confidential',
  COMMENT COLUMN `request_id` 'Claude API request id (materialized from attributes.request_id) — the attribution join key.
@access: confidential',
  COMMENT COLUMN `org_id` 'Gram org id (materialized from attributes.gram.org.id) — the attribution tuple join scope. The tuple is keyed by org, not project, because the hooks key and the OTEL exporter key can resolve different projects.
@access: confidential';
ALTER TABLE `gram`.`trace_summaries`
  COMMENT COLUMN `trace_id` '@access: confidential',
  COMMENT COLUMN `gram_project_id` '@access: confidential',
  COMMENT COLUMN `gram_deployment_id` '@access: confidential',
  COMMENT COLUMN `gram_function_id` '@access: confidential',
  COMMENT COLUMN `gram_urn` '@access: confidential',
  COMMENT COLUMN `start_time_unix_nano` '@access: confidential',
  COMMENT COLUMN `log_count` '@access: confidential',
  COMMENT COLUMN `http_status_code` '@access: confidential',
  COMMENT COLUMN `tool_name` '@access: confidential',
  COMMENT COLUMN `tool_source` '@access: confidential',
  COMMENT COLUMN `event_source` '@access: confidential',
  COMMENT COLUMN `user_email` '@access: confidential-pii',
  COMMENT COLUMN `hook_source` '@access: confidential',
  COMMENT COLUMN `skill_name` '@access: confidential',
  COMMENT COLUMN `has_result` '@access: confidential',
  COMMENT COLUMN `has_error` '@access: confidential',
  COMMENT COLUMN `has_block` '@access: confidential',
  COMMENT COLUMN `block_reason` '@access: opaque-restricted',
  COMMENT COLUMN `toolset_slug` '@access: confidential',
  COMMENT COLUMN `external_user_id` '@access: confidential-pii',
  COMMENT COLUMN `user_id` '@access: confidential-pii',
  COMMENT COLUMN `agent_id` '@access: confidential',
  COMMENT COLUMN `mcp_match` '@access: opaque-restricted',
  COMMENT COLUMN `mcp_server_url` '@access: opaque-restricted',
  COMMENT COLUMN `account_type` '@access: confidential',
  COMMENT COLUMN `provider` '@access: confidential',
  COMMENT COLUMN `meta_mcp_server_id` '@access: confidential',
  COMMENT COLUMN `mcp_client_name` '@access: confidential',
  COMMENT COLUMN `mcp_client_version` '@access: confidential';
