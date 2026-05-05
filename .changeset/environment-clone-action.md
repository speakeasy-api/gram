---
"dashboard": patch
---

Add a Clone action to environment cards in the Environments page. The clone
dialog lets users pick a new name and choose whether to copy only the variable
names (with empty placeholders) or duplicate the encrypted secret values from
the source. Encrypted secret values are never decrypted during the clone —
ciphertext is copied row-to-row inside Postgres. Clone is gated by a project-
level `environment:write` scope plus a per-resource read check on the source
environment (either an `environment:read` grant on that specific env or a
`project:read` grant on the project).
