---
"server": minor
---

Add assistant memory: per-assistant long-term memory backed by vector embeddings. Agents can remember, recall, and forget facts across threads via three new platform tools (gated by the `assistant_memory` product feature). Includes a management API for listing and deleting memories, and a background reaper that hard-deletes soft-deleted rows on schedule.
