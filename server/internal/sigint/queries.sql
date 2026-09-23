-- name: LockSigintProject :exec
-- Serialize every sigint configuration mutation in a project. The fixed seed
-- namespaces this lock independently from other project-scoped subsystems.
SELECT pg_advisory_xact_lock(hashtextextended(@project_id::text, 1397313108));

-- name: CreateSignal :one
INSERT INTO sigint_custom_signals (
    id,
    project_id,
    name,
    description,
    classifier_criteria
)
VALUES (
    @id,
    @project_id,
    @name,
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
    description,
    instructions,
    mode
)
VALUES (
    @id,
    @project_id,
    @name,
    sqlc.narg('description'),
    sqlc.narg('instructions'),
    @mode
)
RETURNING *;

-- name: GetSensor :one
SELECT
    sensor.id,
    sensor.project_id,
    sensor.name,
    sensor.description,
    sensor.instructions,
    sensor.mode,
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
    sensor.description,
    sensor.instructions,
    sensor.mode,
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
    description = sqlc.narg('description'),
    instructions = sqlc.narg('instructions'),
    mode = @mode,
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
    sensor.description,
    sensor.instructions,
    sensor.mode,
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
    sensor.description,
    sensor.instructions,
    sensor.mode,
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
