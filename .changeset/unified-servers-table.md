---
"server": minor
"dashboard": minor
---

The Shadow MCP page becomes one servers table: the inventory list now unions in review-only targets (requested-but-unobserved URLs and stdio commands, marked by a new target_kind field) on the first page, and the separate Access Requests tab is gone. Every row carries its review state with pending decisions sorted first and filterable; URL rows open the server page, stdio rows open the review sheet.
