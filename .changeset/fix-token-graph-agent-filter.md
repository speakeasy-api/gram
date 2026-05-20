---
"server": patch
---

Fix token graph blanking when filtering by agent type on /insights/costs. Claude Code usage metrics were missing the hook_source attribute, causing the filter to return no data for non-cursor agents.
