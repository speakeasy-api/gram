---
"server": minor
---

Add the `identity` service and the `Identity` URN that addresses one subject across every subsystem.

Gram records activity for the same person under several identifiers: audit logs and chats hold a Gram user id, RBAC role assignments hold a WorkOS user id, telemetry and cost aggregate by email, and risk findings key on the external user id an agent reported. `GET /rpc/identity.resolve` takes an identity URN of the form `<kind>:<id>` — `user`, `email`, `external`, `apikey`, or `agent` — and returns every identifier the subject's activity is stored under, plus their directory attributes and the canonical URN to navigate to. Any URN for the same subject resolves to the same identity, so a surface can link with whichever identifier it holds.

The employee identity fold that telemetry used internally now lives in `internal/identity` and backs both callers, so the two cannot drift.
