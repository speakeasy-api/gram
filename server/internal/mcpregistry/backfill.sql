-- Operator-only expansion backfill. Never executed at application startup.
BEGIN;
LOCK TABLE mcp_registry_entries, mcp_registry_servers IN SHARE ROW EXCLUSIVE MODE;
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM mcp_registry_entries WHERE NOT COALESCE(
   jsonb_typeof(data #> '{server,name}') = 'string' AND data #>> '{server,name}' <> '', false)) THEN
   RAISE EXCEPTION 'registry backfill requires valid nonempty server names';
 END IF;
END $$;
WITH inserted AS (
 INSERT INTO mcp_registry_servers(id, name, visible, default_entry_id)
 SELECT id, data #>> '{server,name}', true, CASE WHEN published THEN id END
 FROM mcp_registry_entries WHERE server_id IS NULL
 ON CONFLICT (name) DO NOTHING RETURNING id
)
SELECT count(*) FROM inserted;
UPDATE mcp_registry_entries e SET server_id = s.id
FROM mcp_registry_servers s
WHERE e.server_id IS NULL AND s.name = e.data #>> '{server,name}';
COMMIT;
