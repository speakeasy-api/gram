---
"server": patch
"dashboard": patch
---

Replace the Agent Sessions always-red "N risk" indicator with a
severity-graded count. Findings are now deduped by (policy, source, rule,
matched value) instead of counting every raw occurrence — a single secret
or PII value repeated many times in a pasted log now reads as one finding,
not dozens. The indicator keeps the editorial number treatment, but colors
it by the highest severity band present (low/medium/high, matching
SeverityBadge), with the per-band breakdown in the tooltip — so a session
with only low-severity findings (e.g. an IP address) reads as informational
rather than threatening.
