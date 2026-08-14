---
id: public-surface
kind: exemption
summary: An unauthenticated endpoint where an issuer or slug is the authoritative scope.
---

## When to use it

The query backs an endpoint that serves callers with no session at all — OAuth
client metadata documents, CIMD lookups, MCP slug resolution, public
`.well-known` documents. There is no authenticated tenant, because establishing
one is what the surrounding flow is for.

## Why it is safe

The scope is the issuer id or public slug in the request, and the data returned
is public by construction: an OAuth client metadata document is designed to be
fetched by anyone who knows the client id.

The safety claim is therefore about the _columns_, not the rows. It is fine for
an unauthenticated caller to reach any row in the table, provided every field the
query selects is already public. That is a much narrower claim than "this
endpoint is public", and it is the one that has to hold.

## Evidence required

Name the endpoint and state that it is unauthenticated by design. Then state
which field carries the scope, and confirm that every selected column is public.

`SELECT *` is the recurring hazard here. It is safe on the day it is written and
silently stops being safe the moment someone adds a secret column to the table,
with no diff to this query for anyone to review. Prefer an explicit column list,
and if the query does use `*`, the reason text should say why that is acceptable
for this table.

## Example

```sql
-- name: GetUserSessionClientByClientID :one
-- sqlclint:ignore public-surface -- serves the unauthenticated OAuth client
-- metadata endpoint; issuer_id is the authoritative scope and every selected
-- column is part of the public client document
SELECT client_id, client_name, redirect_uris, issuer_id
FROM user_session_clients
WHERE client_id = @client_id AND deleted IS FALSE;
```

## When not to use it

Do not use it for an endpoint that merely skips a scoping check while still
running behind authentication. If a session exists, it carries a tenant, and the
query should bind it.

Do not use it for a query that returns anything a tenant would consider theirs —
names, counts, timestamps that reveal activity. "The endpoint is public" is not
the claim; "these columns are public" is.
