---
"dashboard": patch
---

Relabel the amber badge that marks platform-admin-only UI from "Dev" to
"Internal Admin". The badge appears on the platform admin toolbar and on
admin-only fields of the device agent configuration page; "Dev" read as
"development build" rather than "visible to Speakeasy staff only", which
is what it actually means. Renamed the component to `InternalAdminBadge`
to match.
