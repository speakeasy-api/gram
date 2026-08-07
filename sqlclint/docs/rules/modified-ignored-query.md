---
id: modified-ignored-query
kind: diagnostic
summary: A grandfathered query's body changed, so its exemption no longer applies.
severity: error
silenced_by: []
---

## What it checks

Each ignore-file entry pins the hash of the query body it was recorded against.
This rule fires when a grandfathered query's body no longer matches its hash.

## Why it matters

This is what makes the ignore file a ratchet rather than a blanket pass.

Entries are keyed by query name, so without a hash the exemption would follow the
name rather than the SQL. A query grandfathered as an unscoped single-row lookup
could be rewritten into an unscoped bulk delete and stay silently exempt, because
the name never changed. Every existing entry would become a permanent licence to
write arbitrary unscoped SQL under that name — the opposite of what a debt list
is for.

Pinning the hash confines each exemption to the exact SQL that was surveyed. The
moment the query changes, it has to earn its place again: fix it, or make a fresh
decision about it.

## How to fix

Look at what the edit did, because the change is your opportunity to clear the
entry rather than renew it.

If the query is now scoped, run `sqlclint run --write-ignore-file`; the entry
disappears. If it still cannot be scoped and you now understand why, replace the
ignore entry with a `sqlclint:ignore` annotation — a reviewed, permanent record
in place of tracked debt. Only if it remains unresolved debt should you
regenerate the file and keep it, with a refreshed hash.

The one thing not to do is edit the hash by hand. That defeats the entire
mechanism, and it is invisible in review.

## Examples

### Violation

The entry was recorded against a scalar lookup:

```sql
-- name: DeleteChatResolutions :execrows
DELETE FROM chat_resolutions WHERE id = @id;
```

and the query has since become:

```sql
-- name: DeleteChatResolutions :execrows
DELETE FROM chat_resolutions WHERE chat_id = ANY(@chat_ids::uuid[]);
```

Same name, same entry, far broader blast radius.

### Compliant

```sql
-- name: DeleteChatResolutions :execrows
DELETE FROM chat_resolutions cr
USING chats c
WHERE c.id = cr.chat_id
  AND cr.chat_id = ANY(@chat_ids::uuid[])
  AND c.project_id = @project_id;
```

Now scoped, so `--write-ignore-file` drops the entry entirely.

## Exemptions

None. The hash mismatch is the signal; silencing it would remove the ratchet.
