---
"server": minor
"admin": minor
---

Add a staff-only `POST /admin/users.block` action that atomically blacklists a user and cascades all local database revocations and sole-member organization disables, invalidates Redis dashboard session epochs and revoked access token JTIs, updates WorkOS user metadata, revokes all WorkOS AuthKit sessions, and adds the user's email to the WorkOS Radar email blocklist with structured per-step partial-failure reporting.
