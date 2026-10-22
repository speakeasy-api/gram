# Restore SQL for one project ($p) from a -delete-orphan-wrapper-servers journal (read with jq -s).
# Replays each server with a committed record, a commit_unknown record, or an intent with no outcome record, newest first; one statement per line.
# commit_unknown and intent-only servers get a -- comment line first; their statements are no-ops if the delete never committed.
# Soft-deleted rows come back only while deleted_at still equals the value this tool set; re-inserted rows only while their server is live.
# A plugin entry is skipped when its plugin already has a live entry with its display name or server; its comment line flags it.
# A policy's scope and updated_at come back only while both still equal what this tool left.
# Only the deleted server's own session client links are journaled; other servers' unservable links the same prune removed are not restored.
def q: "'" + (if type == "string" then . else tojson end | gsub("'"; "''")) + "'";
def live($proj): "EXISTS (SELECT 1 FROM mcp_servers AS s WHERE s.id = r.mcp_server_id AND s.project_id = \($proj) AND s.deleted IS FALSE)";
def restore($proj):
  .row_id as $id
  | if .removed.row == null then "-- no row image: mcp_servers \(.row_id); check by hand"
  else
    "UPDATE mcp_servers SET deleted_at = NULL WHERE id = \(.row_id | q) AND project_id = \($proj) AND deleted_at = \(.removed.soft_deleted_at | q)::timestamptz;",
    (.dependents.gateway_memberships // [] | .[]
      | "UPDATE meta_mcp_server_members AS m SET deleted_at = NULL, updated_at = r.updated_at FROM jsonb_populate_record(NULL::meta_mcp_server_members, \(.row | q)::jsonb) AS r WHERE m.id = r.id AND m.project_id = \($proj) AND m.deleted_at = \(.soft_deleted_at | q)::timestamptz AND \(live($proj));"),
    (.dependents.plugin_members // [] | .[]
      | "-- plugin_servers \(.id) stays deleted if its plugin has a live entry with the same name or server; check by hand",
        "UPDATE plugin_servers AS ps SET deleted_at = NULL, updated_at = r.updated_at FROM plugins AS pl, jsonb_populate_record(NULL::plugin_servers, \(.row | q)::jsonb) AS r WHERE ps.id = r.id AND pl.id = ps.plugin_id AND pl.project_id = \($proj) AND ps.deleted_at = \(.soft_deleted_at | q)::timestamptz AND \(live($proj)) AND NOT EXISTS (SELECT 1 FROM plugin_servers AS c WHERE c.plugin_id = ps.plugin_id AND c.id <> ps.id AND c.deleted IS FALSE AND (c.display_name = ps.display_name OR c.mcp_server_id = ps.mcp_server_id));"),
    (.dependents.assistant_attachments // [] | .[]
      | "INSERT INTO assistant_mcp_servers SELECT (r).* FROM jsonb_populate_record(NULL::assistant_mcp_servers, \(.row | q)::jsonb) AS r JOIN assistants AS a ON a.id = r.assistant_id AND a.project_id = r.project_id WHERE r.project_id = \($proj) AND \(live($proj)) ON CONFLICT DO NOTHING;"),
    (.dependents.risk_policy_scopes // [] | .[]
      | "-- risk_policies \(.policy_id) keeps its scope if anything else changed it after this tool; check by hand",
        "UPDATE risk_policies SET mcp_scope = \(.scope_before | q)::jsonb, updated_at = \(.updated_at_before | q)::timestamptz WHERE id = \(.policy_id | q) AND project_id = \($proj) AND deleted IS FALSE AND updated_at = \(.updated_at_after | q)::timestamptz AND mcp_scope = \(.scope_after | q)::jsonb AND EXISTS (SELECT 1 FROM mcp_servers AS s WHERE s.id = \($id | q) AND s.project_id = \($proj) AND s.deleted IS FALSE);"),
    (.dependents.session_client_links // [] | .[]
      | "INSERT INTO mcp_server_remote_session_clients (id, project_id, organization_id, mcp_server_id, remote_session_client_id, client_attachment_scope, created_at, updated_at, deleted_at) SELECT r.id, r.project_id, r.organization_id, r.mcp_server_id, r.remote_session_client_id, r.client_attachment_scope, r.created_at, r.updated_at, r.deleted_at FROM jsonb_populate_record(NULL::mcp_server_remote_session_clients, \(.row | q)::jsonb) AS r WHERE r.project_id = \($proj) AND \(live($proj)) ON CONFLICT DO NOTHING;")
  end;
map(select(.project_id == $p)) as $records
| ($records | map(select(.event != "intent") | .row_id)) as $settled
| ($p | q) as $proj
| $records | reverse[]
| if .event == "committed" then restore($proj)
  elif .event == "commit_unknown" then "-- commit_unknown: mcp_servers \(.row_id); check by hand", restore($proj)
  elif .event == "intent" and (.row_id as $id | $settled | index($id) | not) then "-- intent without outcome: mcp_servers \(.row_id); check by hand", restore($proj)
  else empty
  end
