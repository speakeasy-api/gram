---
"server": minor
---

Carry Slack file attachment metadata through trigger ingestion into the assistant turn context. Message events that share files (e.g. the `file_share` subtype) now surface each attachment's id, name, mimetype, and size in the turn's message-context block, and the `files` list is addressable from Slack trigger CEL filters. Metadata only — file contents are not fetched.
