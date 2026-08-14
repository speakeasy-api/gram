---
id: unknown-exemption-category
kind: diagnostic
summary: A sqlclint:ignore annotation names a category that is not in the catalog.
severity: error
silenced_by: []
---

## What it checks

A `-- sqlclint:ignore <category> -- <reason>` annotation names a category with no
matching document in the rule catalog.

## Why it matters

The exemption vocabulary is fixed so that exemptions stay reviewable in
aggregate. Anyone can answer "how many queries skip tenancy because they are
keyed by a credential?" by counting `token-keyed` annotations — but only while
that name means one thing.

An open vocabulary erodes quickly. `token-keyed`, `token_keyed`, `by-token`, and
`auth-token` are four spellings of one idea, and once they coexist no count is
trustworthy and no audit is complete. Rejecting unknown categories keeps the
answer to that question a single search.

It also catches the more common case: a typo that would otherwise have created a
new de facto category out of a misspelling.

## How to fix

Run `sqlclint rules --kind exemption` for the current list and pick the category
that actually describes the query. Read its document first — each one states the
evidence its reason text is expected to carry, and picking the closest-sounding
name without reading it tends to produce an exemption that does not survive
review.

If no category fits, that is worth saying out loud rather than working around. A
genuinely new class of cross-tenant query deserves its own document in
`sqlclint/docs/rules/`, added deliberately and reviewed once, rather than a name
invented at a call site.

## Examples

### Violation

```sql
-- name: GetAPIKeyByKeyHash :one
-- sqlclint:ignore auth-token -- the key hash is the credential
SELECT * FROM api_keys WHERE key_hash = @key_hash;
```

### Compliant

```sql
-- name: GetAPIKeyByKeyHash :one
-- sqlclint:ignore token-keyed -- the key hash is the credential being authenticated;
-- there is no tenant known before this lookup resolves one
SELECT * FROM api_keys WHERE key_hash = @key_hash;
```

## Exemptions

None. A malformed exemption cannot exempt itself.
