# Restore SQL for one project ($p) from a -cleanup-orphans journal (read with jq -s).
# Replays only rows with a committed record, newest first; one statement per line.
# commit_unknown rows, and intents with no outcome record, are listed as comments for manual review.
def q: "'" + (if type == "string" then . else tojson end | gsub("'"; "''")) + "'";
map(select(.project_id == $p)) as $records
| ($records | map(select(.event != "intent") | .row_id)) as $settled
| ($p | q) as $proj
| (
    ($records[] | select(.event == "commit_unknown") | "-- commit_unknown: \(.removed.table) \(.row_id); check by hand"),
    ($records[] | select(.event == "intent" and (.row_id as $id | $settled | index($id) | not)) | "-- intent without outcome: \(.removed.table) \(.row_id); check by hand"),
    ($records | reverse[] | select(.event == "committed") | .removed as $r
      | if $r.table == "mcp_metadata" then
          "INSERT INTO mcp_metadata SELECT (r).* FROM jsonb_populate_record(NULL::mcp_metadata, \($r.row | q)::jsonb) AS r WHERE r.project_id = \($proj) ON CONFLICT (id) DO NOTHING;",
          "INSERT INTO mcp_environment_configs SELECT (c).* FROM jsonb_populate_recordset(NULL::mcp_environment_configs, \($r.environment_configs // [] | q)::jsonb) AS c WHERE c.project_id = \($proj) ON CONFLICT (id) DO NOTHING;"
        elif $r.table == "plugin_servers" then
          "UPDATE plugin_servers AS ps SET deleted_at = img.deleted_at, updated_at = img.updated_at FROM plugins AS pl, jsonb_populate_record(NULL::plugin_servers, \($r.row | q)::jsonb) AS img WHERE ps.id = img.id AND pl.id = ps.plugin_id AND pl.project_id = \($proj) AND ps.deleted_at = \($r.soft_deleted_at | q)::timestamptz;"
        else
          "INSERT INTO assistant_toolsets SELECT (r).* FROM jsonb_populate_record(NULL::assistant_toolsets, \($r.row | q)::jsonb) AS r WHERE r.project_id = \($proj) ON CONFLICT (id) DO NOTHING;"
        end)
  )
