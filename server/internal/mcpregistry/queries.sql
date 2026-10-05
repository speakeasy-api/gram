-- name: GetEntry :one
SELECT *
FROM mcp_registry_entries
WHERE id = @id;

-- name: GetEntryByName :one
SELECT *
FROM mcp_registry_entries
WHERE data #>> '{server,name}' = sqlc.arg(name)::text;

-- name: RegistryReady :one
SELECT EXISTS (SELECT 1 FROM mcp_registry_entries LIMIT 1);

-- name: ListEntries :many
-- Budget metadata first; never transfer oversized records or a page of full bodies.
-- Limit candidate metadata before measuring stored bodies.
WITH candidates AS MATERIALIZED (
    SELECT
        id,
        (data #>> '{server,name}')::text AS name,
        published,
        updated_at
    FROM mcp_registry_entries
    WHERE strpos(lower((data #>> '{server,name}')), lower(sqlc.arg(search)::text)) > 0
    AND (sqlc.narg(published)::boolean IS NULL OR published = sqlc.narg(published)::boolean)
    AND (
        NOT sqlc.arg(has_cursor)::boolean
        OR (
            (data #>> '{server,name}') COLLATE "C",
            id
        ) > (
            sqlc.arg(last_name)::text COLLATE "C",
            sqlc.arg(last_id)::uuid
        )
    )
    ORDER BY (data #>> '{server,name}') COLLATE "C", id
    LIMIT @page_limit
), sized AS MATERIALIZED (
    -- Measure only the bounded candidate set.
    SELECT
        c.*,
        octet_length(e.data::text)::bigint AS data_bytes
    FROM candidates c
    JOIN mcp_registry_entries e ON e.id = c.id
), budgeted AS (
    -- Accumulate bytes in cursor order before fetching page bodies.
    SELECT
        *,
        (sum(CASE
            WHEN data_bytes <= sqlc.arg(byte_budget)::bigint THEN data_bytes
            ELSE 0
        END)
        OVER (ORDER BY name COLLATE "C", id))::bigint AS page_bytes
    FROM sized
)
-- Fetch full bodies only when admitted by the byte budget.
SELECT
    b.id,
    b.name,
    b.published,
    b.updated_at,
    b.data_bytes,
    b.page_bytes,
    CASE
        WHEN b.page_bytes <= sqlc.arg(byte_budget)::bigint
            AND b.data_bytes <= sqlc.arg(byte_budget)::bigint
        THEN (SELECT e.data FROM mcp_registry_entries e WHERE e.id = b.id)
        ELSE NULL::jsonb
    END AS data
FROM budgeted b
ORDER BY b.name COLLATE "C", b.id;

-- name: InsertRegistryEntryFixture :exec
-- Test fixture: preserve arbitrary stored records, including invalid records.
INSERT INTO mcp_registry_entries(id, data, published)
VALUES(@id, @data, @published);

-- name: InsertRegistryNamedEntryFixture :exec
INSERT INTO mcp_registry_entries(data)
VALUES(jsonb_build_object('server',jsonb_build_object('name',@name::text)));

-- name: InsertRegistryPublicationFixture :exec
INSERT INTO mcp_registry_entries(data, published)
SELECT
    jsonb_build_object('server',jsonb_build_object('name','io.example/'||n)),
    n%2=0
FROM generate_series(1,60) n;

-- name: CreateEntry :one
INSERT INTO mcp_registry_entries(data, published, published_at)
SELECT
    sqlc.arg(data)::jsonb,
    true,
    sqlc.arg(published_at)::timestamptz
WHERE octet_length(sqlc.arg(data)::jsonb::text) <= sqlc.arg(stored_record_limit)::bigint
RETURNING *;

-- name: LockEntry :one
SELECT *
FROM mcp_registry_entries
WHERE id = @id
FOR UPDATE;

-- name: UpdateEntry :one
UPDATE mcp_registry_entries
SET
    data = @data,
    updated_at = GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')
WHERE id = @id
AND octet_length(sqlc.arg(data)::jsonb::text) <= sqlc.arg(stored_record_limit)::bigint
RETURNING *;

-- name: SetEntryPublished :one
UPDATE mcp_registry_entries
SET
    published = @published,
    published_at = CASE WHEN NOT published AND @published THEN COALESCE(published_at, sqlc.narg(published_at)::timestamptz) ELSE published_at END,
    data = sqlc.arg(data)::jsonb,
    updated_at = GREATEST(clock_timestamp(), updated_at + interval '1 microsecond')
WHERE id = @id
AND (NOT @published::boolean OR octet_length(sqlc.arg(data)::jsonb::text) <= sqlc.arg(stored_record_limit)::bigint)
RETURNING *;

-- name: SerializedRegistryRecordBytes :one
SELECT octet_length(sqlc.arg(data)::jsonb::text);

-- name: CountRegistryEntries :one
SELECT count(*) FROM mcp_registry_entries;

-- name: LockOktaMappings :exec
-- Serialize writers that claim OIN names; no index can enforce element-wise
-- uniqueness on a jsonb array. The conflict scan below relies on READ COMMITTED
-- taking its snapshot after this lock is granted.
SELECT pg_advisory_xact_lock(hashtextextended('mcp_registry_entries:com.speakeasy.ai/okta', 0));

-- name: ListOktaMappingConflicts :many
-- Invalid historical rows may hold a non-array, non-string elements or no
-- name; none of those can claim or be blamed for a key.
SELECT DISTINCT
    (n.value #>> '{}')::text AS oin_name,
    COALESCE(e.data #>> '{server,name}', '')::text AS entry_name
FROM mcp_registry_entries e
CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(e.data #> '{_meta,com.speakeasy.ai/okta,oinNames}') = 'array'
        THEN e.data #> '{_meta,com.speakeasy.ai/okta,oinNames}'
        ELSE '[]'::jsonb
    END
) AS n(value)
WHERE e.id <> sqlc.arg(id)::uuid
AND jsonb_typeof(n.value) = 'string'
AND (n.value #>> '{}') = ANY(sqlc.arg(names)::text[]);

-- name: DiscoverEntries :many
-- Limit candidate metadata before measuring stored bodies.
WITH candidates AS MATERIALIZED (
    SELECT
        id,
        COALESCE(data #>> '{server,name}', '')::text AS discovery_name
    FROM mcp_registry_entries
    WHERE published
    -- Permanently schema-invalid names cannot be cursor keys.
    AND jsonb_typeof(data #> '{server,name}') = 'string'
    AND octet_length(data #>> '{server,name}') BETWEEN 3 AND 200
    AND (data #>> '{server,name}') COLLATE "C" ~ '^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$'
    AND (sqlc.arg(include_deleted)::boolean OR COALESCE(data #>> '{_meta,io.modelcontextprotocol.registry/official,status}', '') <> 'deleted')
    AND strpos(lower(data #>> '{server,name}'), lower(sqlc.arg(search)::text)) > 0
    AND (sqlc.arg(version)::text IN ('', 'latest') OR data #>> '{server,version}' = sqlc.arg(version)::text)
    AND (data #>> '{server,name}') COLLATE "C" > sqlc.arg(after_name)::text COLLATE "C"
    ORDER BY (data #>> '{server,name}') COLLATE "C"
    LIMIT sqlc.arg(page_limit)
), sized AS MATERIALIZED (
    -- Measure only the bounded candidate set.
    SELECT
        c.*,
        octet_length(e.data::text)::bigint AS data_bytes
    FROM candidates c
    JOIN mcp_registry_entries e ON e.id = c.id
), budgeted AS (
    -- Accumulate bytes in cursor order before fetching page bodies.
    SELECT
        *,
        (sum(data_bytes) OVER (ORDER BY discovery_name COLLATE "C"))::bigint AS page_bytes
    FROM sized
)
-- Fetch full bodies only when admitted by the byte budget.
SELECT
    b.discovery_name,
    b.data_bytes,
    b.page_bytes,
    CASE
        WHEN b.page_bytes <= sqlc.arg(byte_budget)::bigint
        THEN (SELECT e.data FROM mcp_registry_entries e WHERE e.id = b.id)
        ELSE NULL::jsonb
    END AS data
FROM budgeted b
ORDER BY b.discovery_name COLLATE "C";

-- name: DiscoverVersion :one
SELECT * FROM mcp_registry_entries
WHERE published
AND data #>> '{server,name}' = sqlc.arg(name)::text
AND (sqlc.arg(include_deleted)::boolean OR COALESCE(data #>> '{_meta,io.modelcontextprotocol.registry/official,status}', '') <> 'deleted')
AND (sqlc.arg(version)::text = 'latest' OR data #>> '{server,version}' = sqlc.arg(version)::text);

-- name: SetRegistryEntryPublishedFixture :exec
UPDATE mcp_registry_entries SET published = @published WHERE id = @id;
