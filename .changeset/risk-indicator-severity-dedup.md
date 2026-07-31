---
"server": patch
"dashboard": patch
---

Fix the Agent Sessions "N risk" indicator: it now dedupes findings by
(source, rule, matched value) instead of counting every raw occurrence — a
single secret or PII value repeated many times in a pasted log now reads as
one finding, not dozens. The indicator's color is also graded by the highest
severity among a session's active findings instead of always rendering
alarming red, so a session with only low/medium-severity findings (e.g. an
IP address) reads as informational rather than threatening.
