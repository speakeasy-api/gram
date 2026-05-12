---
"server": minor
"dashboard": minor
---

Let organization admins exclude specific team members from agent session capture. Adds `productFeatures.listSessionCaptureExclusions` / `productFeatures.setSessionCaptureExclusions` endpoints backed by a new `session_capture_exclusions` table; the hook ingestion path now skips chat-message writes for excluded users even when the org-level session capture flag is on. The Roles & Permissions members table renders a "Logging exclusion" badge for any excluded member.
