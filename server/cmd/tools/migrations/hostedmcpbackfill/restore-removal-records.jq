# Restore SQL for one project ($p) from a -rekey-removal-records journal (read with jq -s).
# Puts each committed record back under its toolset key, newest first; one statement per line.
# commit_unknown rows, intents with no outcome record, and journals without rekeyed_updated_at are listed as comments for manual review.
def q: "'" + (if type == "string" then . else tojson end | gsub("'"; "''")) + "'";
map(select(.project_id == $p)) as $records
| ($records | map(select(.event != "intent") | .row_id)) as $settled
| ($p | q) as $proj
| (
    ($records[] | select(.event == "commit_unknown") | "-- commit_unknown: plugin_servers \(.row_id); check by hand"),
    ($records[] | select(.event == "intent" and (.row_id as $id | $settled | index($id) | not)) | "-- intent without outcome: plugin_servers \(.row_id); check by hand"),
    ($records | reverse[] | select(.event == "committed") | .rekeyed as $r
      | if ($r.rekeyed_updated_at // "") == "" then "-- no rekeyed_updated_at: plugin_servers \(.row_id); check by hand" else
        "UPDATE plugin_servers AS ps SET toolset_id = img.toolset_id, mcp_server_id = img.mcp_server_id, updated_at = img.updated_at FROM plugins AS pl, jsonb_populate_record(NULL::plugin_servers, \($r.row | q)::jsonb) AS img WHERE ps.id = img.id AND pl.id = ps.plugin_id AND pl.project_id = \($proj) AND ps.toolset_id IS NULL AND ps.mcp_server_id = \(.to.id | q) AND ps.deleted_at = \($r.soft_deleted_at | q)::timestamptz AND ps.updated_at = \($r.rekeyed_updated_at | q)::timestamptz;" end)
  )
