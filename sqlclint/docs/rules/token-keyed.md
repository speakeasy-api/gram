---
id: token-keyed
kind: exemption
summary: The query is keyed by a secret whose possession is itself the authorization.
---

## When to use it

The lookup is keyed by a hashed credential — an API key hash, a refresh token
hash, a session JTI, an invitation token. These are the queries that _resolve_ a
tenant; there is no tenant to scope by until they return.

## Why it is safe

The key is unguessable and secret, so holding it is the authorization. This is
the one case where "keyed by an id" is not an IDOR, because the id is a
credential rather than an identifier.

That distinction is doing all the work, and it depends on properties outside the
SQL: the value must be high-entropy, must be stored hashed, and must be compared
against the hash rather than a plaintext column. A predictable or plaintext
"token" fails all of this while looking identical in the query.

## Evidence required

Name the credential column and state that it holds a hash of a high-entropy
secret. Then state that this lookup is what establishes the tenant, rather than
running after one is already known.

That second half is what separates a real credential resolution from a lookup
that simply forgot its scope: if a tenant _was_ already known at the call site,
the query should be binding it.

## Example

```sql
-- name: GetAPIKeyByKeyHash :one
-- sqlclint:ignore token-keyed -- key_hash holds a SHA-256 of a high-entropy API
-- key; this lookup is what resolves the organization, so no tenant is known to
-- scope by at call time
SELECT * FROM api_keys WHERE key_hash = @key_hash AND deleted IS FALSE;
```

## When not to use it

Do not use it for a lookup keyed by a UUID primary key. A UUID is unguessable in
practice but it is an identifier, not a credential: it appears in URLs, logs, and
API responses, and it is never rotated or revoked. Rows addressed by one still
need a tenancy bound.

Do not use it for a query that runs _after_ authentication, on a path where the
organization is already in the auth context. Once the tenant is known, bind it.

Do not use it for slugs, short codes, or sequential ids, however secret they are
treated in practice.
