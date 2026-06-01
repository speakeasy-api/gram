---
"server": patch
---

New assistants default to 5 concurrent warm runtimes (was 1) and a 60-second warm TTL (was 300s) so they handle bursts without queueing while letting idle runtimes reclaim resources faster. Existing assistants keep their saved values.
