-- name: LockSigintProject :exec
-- Serialize every sigint configuration mutation in a project. The fixed seed
-- namespaces this lock independently from other project-scoped subsystems.
SELECT pg_advisory_xact_lock(hashtextextended(@project_id::text, 1397313108));

-- name: SaveDefinitionSnapshots :exec
INSERT INTO sigint_definition_snapshots (project_id, sensor_id, definition_hash, definition)
SELECT @project_id::uuid, snapshot.sensor_id, snapshot.definition_hash, snapshot.definition
FROM jsonb_to_recordset(@snapshots::jsonb) AS snapshot(sensor_id uuid, definition_hash text, definition jsonb)
JOIN projects p ON p.id = @project_id::uuid AND p.organization_id = @organization_id
ON CONFLICT (project_id, sensor_id, definition_hash) DO NOTHING;

-- name: GetDefinitionSnapshot :one
SELECT snapshot.definition, snapshot.created_at
FROM sigint_definition_snapshots snapshot
JOIN projects p ON p.id = snapshot.project_id AND p.organization_id = @organization_id
WHERE snapshot.project_id = @project_id::uuid
  AND snapshot.sensor_id = @sensor_id::uuid AND snapshot.definition_hash = @definition_hash;

-- name: ListDefinitionSnapshots :many
SELECT definition_hash, definition, created_at
FROM sigint_definition_snapshots
WHERE project_id = @project_id::uuid AND sensor_id = @sensor_id::uuid
ORDER BY created_at, definition_hash
LIMIT @limit_value;

-- name: LoadEvaluationSensors :many
-- One statement captures all enabled definitions and membership order consistently.
SELECT sensor.id AS sensor_id, sensor.slug AS sensor_slug, sensor.mode, sensor.instructions, sensor.match_expression,
       COALESCE(sensor.input_strategy, 'single_message')::text AS input_strategy,
       COALESCE(sensor.window_size, 5)::integer AS window_size,
       signal.id AS signal_id, signal.slug AS signal_slug, signal.classifier_criteria
FROM sigint_sensors AS sensor
JOIN projects AS project ON project.id = sensor.project_id
LEFT JOIN sigint_sensor_signals AS member
  ON member.project_id = sensor.project_id AND member.sensor_id = sensor.id
  AND member.deleted IS FALSE
LEFT JOIN sigint_custom_signals AS signal
  ON signal.project_id = member.project_id AND signal.id = member.signal_id
  AND signal.deleted IS FALSE
WHERE sensor.project_id = @project_id
  AND project.organization_id = @organization_id
  AND project.deleted IS FALSE
  AND sensor.deleted IS FALSE
  AND sensor.enabled IS TRUE
ORDER BY sensor.id, member.sort_order, member.id;

-- name: CreateSignal :one
INSERT INTO sigint_custom_signals (
    id,
    project_id,
    name,
    slug,
    description,
    classifier_criteria
)
VALUES (
    @id,
    @project_id,
    @name,
    @slug,
    sqlc.narg('description'),
    sqlc.narg('classifier_criteria')
)
RETURNING *;

-- name: GetSignal :one
SELECT *
FROM sigint_custom_signals
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE;

-- name: ListSignals :many
SELECT *
FROM sigint_custom_signals
WHERE project_id = @project_id
  AND deleted IS FALSE
  AND (sqlc.narg('cursor')::uuid IS NULL OR id > sqlc.narg('cursor')::uuid)
ORDER BY id
LIMIT @limit_value;

-- name: UpdateSignal :one
UPDATE sigint_custom_signals
SET name = @name,
    slug = @slug,
    description = sqlc.narg('description'),
    classifier_criteria = sqlc.narg('classifier_criteria'),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteSignal :one
UPDATE sigint_custom_signals
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: CreateSensor :one
INSERT INTO sigint_sensors (
    id,
    project_id,
    name,
    slug,
    description,
    instructions,
    mode,
    enabled,
    match_expression,
    input_strategy,
    window_size
)
VALUES (
    @id,
    @project_id,
    @name,
    @slug,
    sqlc.narg('description'),
    sqlc.narg('instructions'),
    @mode,
    @enabled,
    @match_expression,
    sqlc.narg('input_strategy'),
    sqlc.narg('window_size')
)
RETURNING *;

-- name: GetSensor :one
SELECT
    sensor.id,
    sensor.project_id,
    sensor.name,
    sensor.slug,
    sensor.description,
    sensor.instructions,
    sensor.mode,
    sensor.enabled,
    sensor.match_expression,
    COALESCE(sensor.input_strategy, 'single_message')::text AS input_strategy,
    COALESCE(sensor.window_size, 5)::integer AS window_size,
    COALESCE(membership.signal_ids, ARRAY[]::uuid[])::uuid[] AS signal_ids,
    sensor.created_at,
    sensor.updated_at
FROM sigint_sensors AS sensor
LEFT JOIN LATERAL (
    SELECT array_agg(member.signal_id ORDER BY member.sort_order, member.id) AS signal_ids
    FROM sigint_sensor_signals AS member
    WHERE member.project_id = sensor.project_id
      AND member.sensor_id = sensor.id
      AND member.deleted IS FALSE
) AS membership ON TRUE
WHERE sensor.id = @id
  AND sensor.project_id = @project_id
  AND sensor.deleted IS FALSE;

-- name: ListSensors :many
SELECT
    sensor.id,
    sensor.project_id,
    sensor.name,
    sensor.slug,
    sensor.description,
    sensor.instructions,
    sensor.mode,
    sensor.enabled,
    sensor.match_expression,
    COALESCE(sensor.input_strategy, 'single_message')::text AS input_strategy,
    COALESCE(sensor.window_size, 5)::integer AS window_size,
    COALESCE(membership.signal_ids, ARRAY[]::uuid[])::uuid[] AS signal_ids,
    sensor.created_at,
    sensor.updated_at
FROM sigint_sensors AS sensor
LEFT JOIN LATERAL (
    SELECT array_agg(member.signal_id ORDER BY member.sort_order, member.id) AS signal_ids
    FROM sigint_sensor_signals AS member
    WHERE member.project_id = sensor.project_id
      AND member.sensor_id = sensor.id
      AND member.deleted IS FALSE
) AS membership ON TRUE
WHERE sensor.project_id = @project_id
  AND sensor.deleted IS FALSE
  AND (sqlc.narg('cursor')::uuid IS NULL OR sensor.id > sqlc.narg('cursor')::uuid)
ORDER BY sensor.id
LIMIT @limit_value;

-- name: UpdateSensor :one
UPDATE sigint_sensors
SET name = @name,
    slug = @slug,
    description = sqlc.narg('description'),
    instructions = sqlc.narg('instructions'),
    mode = @mode,
    enabled = @enabled,
    match_expression = @match_expression,
    input_strategy = sqlc.narg('input_strategy'),
    window_size = sqlc.narg('window_size'),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: DeleteSensor :one
UPDATE sigint_sensors
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE id = @id
  AND project_id = @project_id
  AND deleted IS FALSE
RETURNING *;

-- name: ListLiveSignalIDs :many
SELECT id
FROM sigint_custom_signals
WHERE project_id = @project_id
  AND id = ANY(@ids::uuid[])
  AND deleted IS FALSE
ORDER BY id;

-- name: ReplaceSensorSignals :exec
WITH input AS (
    SELECT signal_id, (ordinality - 1)::integer AS sort_order
    FROM unnest(@signal_ids::uuid[]) WITH ORDINALITY AS requested(signal_id, ordinality)
),
updated AS (
    UPDATE sigint_sensor_signals AS member
    SET sort_order = input.sort_order,
        updated_at = clock_timestamp()
    FROM input
    WHERE member.project_id = @project_id
      AND member.sensor_id = @sensor_id
      AND member.signal_id = input.signal_id
      AND member.deleted IS FALSE
    RETURNING member.signal_id
),
deleted AS (
    UPDATE sigint_sensor_signals AS member
    SET deleted_at = clock_timestamp(),
        updated_at = clock_timestamp()
    WHERE member.project_id = @project_id
      AND member.sensor_id = @sensor_id
      AND member.deleted IS FALSE
      AND NOT EXISTS (
          SELECT 1
          FROM input
          WHERE input.signal_id = member.signal_id
      )
    RETURNING member.signal_id
)
INSERT INTO sigint_sensor_signals (
    id,
    project_id,
    sensor_id,
    signal_id,
    sort_order
)
SELECT
    generate_uuidv7(),
    @project_id,
    @sensor_id,
    input.signal_id,
    input.sort_order
FROM input
WHERE NOT EXISTS (
    SELECT 1
    FROM updated
    WHERE updated.signal_id = input.signal_id
);

-- name: DeleteSensorSignals :exec
UPDATE sigint_sensor_signals
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND sensor_id = @sensor_id
  AND deleted IS FALSE;

-- name: ListSensorsForSignal :many
SELECT
    sensor.id,
    sensor.project_id,
    sensor.name,
    sensor.slug,
    sensor.description,
    sensor.instructions,
    sensor.mode,
    sensor.enabled,
    sensor.match_expression,
    COALESCE(sensor.input_strategy, 'single_message')::text AS input_strategy,
    COALESCE(sensor.window_size, 5)::integer AS window_size,
    COALESCE(membership.signal_ids, ARRAY[]::uuid[])::uuid[] AS signal_ids,
    sensor.created_at,
    sensor.updated_at
FROM sigint_sensors AS sensor
JOIN sigint_sensor_signals AS attached
  ON attached.project_id = sensor.project_id
 AND attached.sensor_id = sensor.id
 AND attached.signal_id = @signal_id
 AND attached.deleted IS FALSE
LEFT JOIN LATERAL (
    SELECT array_agg(member.signal_id ORDER BY member.sort_order, member.id) AS signal_ids
    FROM sigint_sensor_signals AS member
    WHERE member.project_id = sensor.project_id
      AND member.sensor_id = sensor.id
      AND member.deleted IS FALSE
) AS membership ON TRUE
WHERE sensor.project_id = @project_id
  AND sensor.deleted IS FALSE
ORDER BY sensor.id;

-- name: DeleteSignalMemberships :many
UPDATE sigint_sensor_signals
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND signal_id = @signal_id
  AND deleted IS FALSE
RETURNING sensor_id;

-- name: CompactSensorSignalOrder :exec
WITH ordered AS (
    SELECT
        id,
        (row_number() OVER (PARTITION BY sensor_id ORDER BY sort_order, id) - 1)::integer AS compact_order
    FROM sigint_sensor_signals
    WHERE project_id = @project_id
      AND sensor_id = ANY(@sensor_ids::uuid[])
      AND deleted IS FALSE
)
UPDATE sigint_sensor_signals AS member
SET sort_order = ordered.compact_order,
    updated_at = clock_timestamp()
FROM ordered
WHERE member.id = ordered.id
  AND member.project_id = @project_id
  AND member.sort_order <> ordered.compact_order;

-- name: TouchSensors :exec
UPDATE sigint_sensors
SET updated_at = clock_timestamp()
WHERE project_id = @project_id
  AND id = ANY(@sensor_ids::uuid[])
  AND deleted IS FALSE;

-- name: GetSensorsByIDs :many
SELECT
    sensor.id,
    sensor.project_id,
    sensor.name,
    sensor.slug,
    sensor.description,
    sensor.instructions,
    sensor.mode,
    sensor.enabled,
    sensor.match_expression,
    COALESCE(sensor.input_strategy, 'single_message')::text AS input_strategy,
    COALESCE(sensor.window_size, 5)::integer AS window_size,
    COALESCE(membership.signal_ids, ARRAY[]::uuid[])::uuid[] AS signal_ids,
    sensor.created_at,
    sensor.updated_at
FROM sigint_sensors AS sensor
LEFT JOIN LATERAL (
    SELECT array_agg(member.signal_id ORDER BY member.sort_order, member.id) AS signal_ids
    FROM sigint_sensor_signals AS member
    WHERE member.project_id = sensor.project_id
      AND member.sensor_id = sensor.id
      AND member.deleted IS FALSE
) AS membership ON TRUE
WHERE sensor.project_id = @project_id
  AND sensor.id = ANY(@ids::uuid[])
  AND sensor.deleted IS FALSE
ORDER BY sensor.id;

-- name: LoadEvaluationMessages :many
-- One tenant-pinned read resolves the batch and its current attachment locators.
-- System messages are excluded for every input strategy.
SELECT m.id, m.chat_id, m.project_id, m.seq, m.generation, m.created_at,
       m.role, m.content, m.content_raw, m.content_asset_url, m.tool_calls,
       m.user_id, m.external_user_id, c.user_account_id,
       COALESCE(m.origin = 'anthropic-inference' AND (
         m.external_message_id LIKE '%/block:%' OR EXISTS (
           SELECT 1 FROM chat_messages sibling
           WHERE sibling.project_id = m.project_id AND sibling.chat_id = m.chat_id
             AND sibling.role <> 'system'
             AND sibling.external_message_id = m.external_message_id || '/block:0'
         )
       ), false)::boolean AS row_local_content,
       ARRAY(SELECT cp.content_asset_url FROM chat_content_parts cp
             WHERE cp.project_id = m.project_id AND cp.chat_id = m.chat_id
               AND cp.parent_chat_message_id = m.id AND cp.deleted IS FALSE
             ORDER BY cp.created_at, cp.id)::text[] AS attachment_uris
FROM chat_messages m
JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
JOIN projects p ON p.id = m.project_id
WHERE m.project_id = @project_id::uuid AND p.organization_id = @organization_id
  AND p.deleted IS FALSE AND c.deleted IS FALSE
  AND m.role <> 'system'
  AND m.id = ANY(@message_ids::uuid[]);

-- name: LoadEvaluationHistory :many
-- Anchor to the target snapshot, never to the latest message in the conversation.
-- Filter system messages before LIMIT so they do not consume window capacity.
-- Bound the cumulative inline payload before transferring it to the application.
-- Keep overflow rows as markers: skipping them would change window membership.
-- Reserve eight bytes per attachment locator for array element framing.
WITH candidates AS MATERIALIZED (
    SELECT m.id, m.project_id, m.chat_id, m.created_at, m.seq,
           m.role, m.content, m.content_raw, m.content_asset_url, m.tool_calls,
           m.origin, m.external_message_id
    FROM chat_messages m
    JOIN chats c ON c.id = m.chat_id AND c.project_id = m.project_id
    JOIN projects p ON p.id = m.project_id
    WHERE m.project_id = @project_id::uuid AND p.organization_id = @organization_id
      AND p.deleted IS FALSE AND c.deleted IS FALSE
      AND m.role <> 'system'
      AND m.chat_id = @chat_id AND m.generation = @generation
      AND m.id <> @target_id
      AND (m.created_at, m.seq) < (@target_created_at::timestamptz, @target_seq::bigint)
    ORDER BY m.created_at DESC, m.seq DESC
    LIMIT @history_limit
), sized AS (
    SELECT m.*,
           (octet_length(m.role)::bigint + octet_length(m.content)::bigint
            + COALESCE(octet_length(m.content_raw::text), 0)::bigint
            + COALESCE(octet_length(m.content_asset_url), 0)::bigint
            + COALESCE(octet_length(m.tool_calls::text), 0)::bigint
            + COALESCE((SELECT sum(octet_length(cp.content_asset_url)::bigint + 8)
                        FROM chat_content_parts cp
                        WHERE cp.project_id = m.project_id AND cp.chat_id = m.chat_id
                          AND cp.parent_chat_message_id = m.id AND cp.deleted IS FALSE), 0)
           ) AS payload_bytes
    FROM candidates m
), bounded AS (
    SELECT m.*,
           (sum(payload_bytes) OVER (ORDER BY created_at DESC, seq DESC ROWS UNBOUNDED PRECEDING)
            > @content_budget::bigint)::boolean AS content_too_large
    FROM sized m
)
SELECT m.id,
       (CASE WHEN m.content_too_large THEN '' ELSE m.role END)::text AS role,
       (CASE WHEN m.content_too_large THEN '' ELSE m.content END)::text AS content,
       (CASE WHEN m.content_too_large THEN NULL ELSE m.content_raw END)::jsonb AS content_raw,
       (CASE WHEN m.content_too_large THEN '' ELSE COALESCE(m.content_asset_url, '') END)::text AS content_asset_url,
       (CASE WHEN m.content_too_large THEN NULL ELSE m.tool_calls END)::jsonb AS tool_calls,
       COALESCE(m.origin = 'anthropic-inference' AND (
         m.external_message_id LIKE '%/block:%' OR EXISTS (
           SELECT 1 FROM chat_messages sibling
           WHERE sibling.project_id = m.project_id AND sibling.chat_id = m.chat_id
             AND sibling.role <> 'system'
             AND sibling.external_message_id = m.external_message_id || '/block:0'
         )
       ), false)::boolean AS row_local_content,
        CASE WHEN m.content_too_large THEN ARRAY[]::text[] ELSE
        ARRAY(SELECT cp.content_asset_url FROM chat_content_parts cp
             WHERE cp.project_id = m.project_id AND cp.chat_id = m.chat_id
               AND cp.parent_chat_message_id = m.id AND cp.deleted IS FALSE
              ORDER BY cp.created_at, cp.id)::text[] END AS attachment_uris,
        m.content_too_large
FROM bounded m
ORDER BY m.created_at DESC, m.seq DESC;
