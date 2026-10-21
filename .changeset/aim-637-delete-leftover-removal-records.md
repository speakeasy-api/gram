---
"server": patch
---

Add a `-delete-leftover-removal-records` mode to the hosted MCP wrappers migration tool. It hard-deletes soft-deleted plugin removal records that are still keyed only by toolset when their toolset or project is deleted, or when the plugin already has a server-keyed removal record for the toolset's canonical server, and leaves any record that may still keep a live wrapper server of the toolset out, so the toolset column can be dropped. It runs as a dry run by default, journals every deleted row, and ships a restore script. It also adds a `-delete-orphan-wrapper-servers` mode, run first, that deletes live non-canonical servers left behind by deleted toolsets through the app's server delete, with the same audit rows and publication requests, a journal and a restore script, so their toolsets' removal records can then be deleted too. Every run of the tool, dry runs included, now needs an explicit `-project <id>` or `-all-projects`.
