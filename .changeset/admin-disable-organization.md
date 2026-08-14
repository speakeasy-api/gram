---
"server": patch
---

Platform admins can now disable an organization and undo it. Until now the only
thing that could disable an organization was the WorkOS `organization.deleted`
webhook, and nothing anywhere could re-enable one, so an operator had no way to
cut off access or to reverse a disable that turned out to be wrong. Two admin
endpoints cover both directions and report the organization back in its new
state, which the existing list and detail endpoints already surface.

Disabling is keyed on the Gram organization id rather than the WorkOS one, so an
organization that was never linked to WorkOS can be disabled like any other.
Disabling an organization that is already disabled keeps the original timestamp
instead of moving it, so the record of when access was cut stays true. Neither
direction touches the whitelist flag, which is the separate not-yet-approved
gate, nor the WorkOS webhook cursor, which only the webhook path may write.

All three organization writes now read the organization back by id alone, which
also corrects the existing update endpoint. An organization id and another
organization's slug can be the same string, and the read that produced the
response resolved either, so a write could return a different organization than
the one it changed. Addressing one of these writes by slug now returns a
not-found instead of a success describing an organization that was never
touched. Reading an organization by slug still works, and when the same string
is one organization's id and another's slug the exact id match now wins instead
of the database picking either row.

The admin dashboard row action and confirmation dialog follow.
