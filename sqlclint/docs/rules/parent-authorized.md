---
id: parent-authorized
kind: exemption
summary: The caller already resolved and authorized the parent row this query hangs off.
---

## When to use it

The query reads a child table — headers on a server, versions of a skill, entries
in an environment — on a hot path where the caller has already loaded the parent
row through a properly scoped query and is now fetching its children by parent
id.

## Why it is safe

The tenancy check happened one step earlier, on the parent. Repeating it here
would mean a second join purely to re-derive a fact the caller already
established.

This is the weakest category in the catalog and deserves the most scepticism.
Every other exemption rests on a property of the data — the rows are public, the
key is a credential, the table is a catalog. This one rests on a property of the
_call sequence_, which nothing enforces and any refactor can break. The
protection disappears silently: no test fails, no type changes, the query simply
starts being called from somewhere that never loaded the parent.

Prefer an `EXISTS` subquery pinning the parent's tenant. It costs an index lookup
and removes the need for this annotation entirely:

```sql
WHERE h.remote_mcp_server_id = @remote_mcp_server_id
  AND EXISTS (
    SELECT 1 FROM remote_mcp_servers s
    WHERE s.id = h.remote_mcp_server_id AND s.project_id = @project_id
  )
```

## Evidence required

Name the exact caller — file and function — that resolves the parent, and state
which scoped query it used. Then say why the subquery form was rejected, since
that is the default and this is the deviation.

Also state that no other caller reaches this query. That is the invariant the
whole exemption rests on, and writing it down is what lets the next person notice
when they are about to break it.

## Example

```sql
-- name: ListHeadersByServerID :many
-- sqlclint:ignore parent-authorized -- serves the MCP proxy in
-- internal/mcp/serveendpoint.go, which has already loaded the remote_mcp_servers
-- row via GetServerByIDAndProject; this is the per-request hot path and the
-- management surface uses the project-scoped ListServerHeaders instead
SELECT * FROM remote_mcp_server_headers
WHERE remote_mcp_server_id = @remote_mcp_server_id
ORDER BY name;
```

## When not to use it

Do not use it for a management or CRUD endpoint. Those are not hot paths, and the
`EXISTS` form costs nothing meaningful there.

Do not use it when the parent was loaded by an unscoped query. Then nothing was
authorized and the exemption is asserting a check that never ran.

Do not use it for a query with more than one caller unless every caller
independently satisfies the invariant — and if that is true, say so explicitly,
because the next caller added will not know to.
