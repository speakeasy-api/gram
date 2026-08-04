---
"server": minor
"dashboard": patch
---

Import Codex cloud task transcripts as agent sessions (DNO-752). A new
codex_cloud_sessions schedule on the chatgpt_compliance integration polls the
workspace-scoped CODEX_LOG compliance feed and persists cloud web-task
prompts and responses as external chats + messages under the new codex-web
chat source, with prompt-derived titles and idempotent replays. Only
CODEX_WEB client events are imported (desktop-app events are counted and
skipped pending the unified-app verification), and the feed's per-turn token
counts are deliberately not persisted — cloud tokens meter through the
compliance COSTS promotion, so carrying them here would double count.
Enforcement over cloud runs remains impossible (post-hoc batch feed); this
provides visibility and post-hoc review only.
