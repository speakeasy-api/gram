---
"server": minor
"dashboard": patch
---

Add a platform-admin surface for the chat analysis pipeline's per-organization settings. A new `adminChatAnalysis` management service (`getSettings` / `upsertWorkUnitsSettings`, session-only, gated on the platform-admin flag) reads and writes the organization's `chat_analysis_settings` row for the work-units judge, taking the same organization advisory lock the reservation transaction holds and recording before/after audit snapshots under the new `chat_analysis_settings` subject. The developer toolkit's Features tab gains a matching "Work Units Chat Analysis" section: an org-wide enable/disable control plus the daily evaluation cap, with a suggested cap prefilled when enabling an organization that never had one. A third method, `triggerAnalysis`, wakes the chat analysis coordinator of every project in the organization on demand — surfaced as a "Run now" button in the same section — so an admin can start a pass immediately instead of waiting for a chat write or the periodic sweep.
