---
"server": patch
---

Persist hook-captured chat messages at their original occurred_at and order transcripts by (created_at, seq) (DNO-536). Previously chat_messages rows were stamped at insert time and read back in insertion order, so downtime backlog replayed from a device's offline spool sorted AFTER the newer live event that triggered the drain — the latest message appeared before older ones. The ingest handler now writes the event's occurred_at (clamped to arrival time so a skewed device clock cannot sort a row into the future) as created_at, and every transcript reader — full lists, keyset pages, risk/search windows — orders by (created_at, seq) with seq as the stable tiebreak. Keyset cursors keep their public seq shape; the anchor row's position is resolved server-side. Non-hook writers (playground, assistants, imports) leave created_at unset and the message store stamps each batch with one shared write-time value, so their ordering semantics are unchanged.
