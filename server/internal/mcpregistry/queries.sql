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

