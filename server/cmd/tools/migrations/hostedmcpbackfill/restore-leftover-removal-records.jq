# Restore SQL for one project ($p) from a -delete-leftover-removal-records journal (read with jq -s).
# Re-inserts each committed row from its image, newest first; one statement per line.
# commit_unknown rows, and intents with no outcome record, are listed as comments for manual review.
# The column list leaves out the generated deleted column; it needs plugin_servers.toolset_id, so it works only before AIM-581.
def q: "'" + (if type == "string" then . else tojson end | gsub("'"; "''")) + "'";
map(select(.project_id == $p)) as $records
| ($records | map(select(.event != "intent") | .row_id)) as $settled
| ($p | q) as $proj
| (
    ($records[] | select(.event == "commit_unknown") | "-- commit_unknown: plugin_servers \(.row_id); check by hand"),
    ($records[] | select(.event == "intent" and (.row_id as $id | $settled | index($id) | not)) | "-- intent without outcome: plugin_servers \(.row_id); check by hand"),
    ($records | reverse[] | select(.event == "committed") | .removed as $r
      | "INSERT INTO plugin_servers (id, plugin_id, project_id, toolset_id, mcp_server_id, meta_mcp_server_id, display_name, policy, sort_order, created_at, updated_at, deleted_at) SELECT r.id, r.plugin_id, r.project_id, r.toolset_id, r.mcp_server_id, r.meta_mcp_server_id, r.display_name, r.policy, r.sort_order, r.created_at, r.updated_at, r.deleted_at FROM jsonb_populate_record(NULL::plugin_servers, \($r.row | q)::jsonb) AS r JOIN plugins AS pl ON pl.id = r.plugin_id AND pl.project_id = \($proj) WHERE r.deleted_at IS NOT NULL ON CONFLICT (id) DO NOTHING;")
  )
