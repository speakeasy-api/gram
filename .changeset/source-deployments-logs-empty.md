---
"dashboard": patch
---

Fix the empty "Logs" panel on a source's Deployments tab. The embedded panel was comparing a kind string (e.g. `"function"`) against log events' `attachmentId` (a UUID) and was also sending the wrong type token (`"function"` instead of the backend's `"functions"`). Now filters on `event.attachmentType` with the correct backend constants (`functions`, `openapi`, `external_mcp`), so per-source deployment logs render as expected.
