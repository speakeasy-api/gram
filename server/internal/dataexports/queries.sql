-- Every query pins both organization_id and project_id. Resource UUIDs and
-- tenant-pinned foreign keys are integrity controls, not authorization bounds.

-- Resolve the active OTEL destination for one project data source.
-- name: GetActiveOtelRouteDestination :one
SELECT
  destination.endpoint_url,
  destination.headers_encrypted,
  COALESCE(destination.sensitive_data, 'exclude') = 'include' AS include_sensitive_data
FROM data_export_routes AS route
JOIN otel_destinations AS destination
  ON destination.organization_id = route.organization_id
 AND destination.project_id = route.project_id
 AND destination.id = route.otel_destination_id
WHERE route.organization_id = @organization_id
  AND route.project_id = @project_id
  AND route.data_source = @data_source
  AND route.enabled IS TRUE
  AND route.deleted IS FALSE
  AND route.otel_destination_id IS NOT NULL
  AND destination.deleted IS FALSE;

-- List active destinations in stable creation order for the management API.
-- name: ListOtelDestinations :many
SELECT *
FROM otel_destinations
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at, id;


-- Lock a destination before merging preserved header secrets or deleting it.
-- The lock keeps the before snapshot and subsequent mutation on one row version.
-- name: GetOtelDestinationForUpdate :one
SELECT *
FROM otel_destinations
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
FOR UPDATE;

-- Hold a shared destination lock while a route is validated and written.
-- Concurrent route writes may share it, but destination updates/deletes wait,
-- closing the soft-delete race that the foreign key cannot prevent.
-- name: GetOtelDestinationForRoute :one
SELECT *
FROM otel_destinations
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
FOR SHARE;

-- Create a tenant-pinned destination and return its API/audit representation.
-- name: CreateOtelDestination :one
INSERT INTO otel_destinations (
  organization_id,
  project_id,
  name,
  endpoint_url,
  headers_encrypted,
  sensitive_data
) VALUES (
  @organization_id,
  @project_id,
  @name,
  @endpoint_url,
  @headers_encrypted,
  @sensitive_data
)
RETURNING *;

-- Replace validated destination configuration and return the after snapshot.
-- name: UpdateOtelDestination :one
UPDATE otel_destinations
SET name = @name,
    endpoint_url = @endpoint_url,
    headers_encrypted = @headers_encrypted,
    sensitive_data = @sensitive_data,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- Detect any non-deleted route reference before destination soft deletion.
-- Disabled routes count because they remain configured and can be re-enabled.
-- name: OtelDestinationHasActiveRoutes :one
SELECT EXISTS (
  SELECT 1
  FROM data_export_routes
  WHERE organization_id = @organization_id
    AND project_id = @project_id
    AND otel_destination_id = @otel_destination_id
    AND deleted IS FALSE
);

-- Tombstone an unreferenced destination while its FOR UPDATE lock is held.
-- name: SoftDeleteOtelDestination :one
UPDATE otel_destinations
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- List active routes in stable creation order for the management API.
-- name: ListDataExportRoutes :many
SELECT *
FROM data_export_routes
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND deleted IS FALSE
ORDER BY created_at, id;

-- Serialize route update/delete transitions so audit snapshots describe the
-- exact row version mutated by the transaction.
-- name: GetDataExportRouteForUpdate :one
SELECT *
FROM data_export_routes
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
FOR UPDATE;

-- Create a route with a nullable OTEL destination. The partial unique index
-- atomically enforces one non-deleted route per project and data source.
-- name: CreateDataExportRoute :one
INSERT INTO data_export_routes (
  organization_id,
  project_id,
  data_source,
  enabled,
  otel_destination_id
) VALUES (
  @organization_id,
  @project_id,
  @data_source,
  @enabled,
  sqlc.narg('otel_destination_id')::uuid
)
RETURNING *;

-- Replace route configuration after locking the route and validating any
-- destination under a shared lock.
-- name: UpdateDataExportRoute :one
UPDATE data_export_routes
SET data_source = @data_source,
    enabled = @enabled,
    otel_destination_id = sqlc.narg('otel_destination_id')::uuid,
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;

-- Atomically lock and tombstone a route, returning the deleted audit subject.
-- name: SoftDeleteDataExportRoute :one
UPDATE data_export_routes
SET deleted_at = clock_timestamp(),
    updated_at = clock_timestamp()
WHERE organization_id = @organization_id
  AND project_id = @project_id
  AND id = @id
  AND deleted IS FALSE
RETURNING *;
