---
"server": minor
---

Intent-policy analyses now export the transcript they judged, so it can be read beside the assessments. The first window of each analysis publishes one `gram.intentpolicy.v1.TranscriptMessage` per message of the analyzed chat generation: role, text, tool calls with their arguments, tool result ids, model and timestamps, attributed to the organization, project and person who ran the analysis. `gram streams` writes them as daily Parquet under `gram.intentpolicy.v1.TranscriptLake/` in a separate `intent-transcripts` bucket. Detected secrets are masked before publishing, a message that cannot be scanned is exported without its content, and very large messages are truncated and marked as such. Rows join to assessments on `chat_id` and `message_id`.
