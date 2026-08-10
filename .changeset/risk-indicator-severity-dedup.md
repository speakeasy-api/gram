---
"server": patch
"dashboard": patch
---

Replace the Agent Sessions "N risk" indicator with a mini severity
histogram. Findings are now deduped by (source, rule, matched value)
instead of counting every raw occurrence — a single secret or PII value
repeated many times in a pasted log now reads as one finding, not dozens.
The indicator itself is a three-bar histogram (low/medium/high severity),
each bar's height a percentage of the session's message count, instead of
a single number that always rendered alarming red — a session with only
low/medium-severity findings (e.g. an IP address) now reads as
informational rather than threatening.
