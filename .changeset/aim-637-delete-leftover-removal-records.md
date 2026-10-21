---
"server": patch
---

Add a `-delete-leftover-removal-records` mode to the hosted MCP wrappers migration tool. It hard-deletes soft-deleted plugin removal records that are still keyed only by toolset when their toolset or project is deleted, or when the plugin already has a server-keyed removal record for the toolset's canonical server, and leaves any record that may still keep a live wrapper server of the toolset out, so the toolset column can be dropped. It runs as a dry run by default, journals every deleted row, and ships a restore script.
