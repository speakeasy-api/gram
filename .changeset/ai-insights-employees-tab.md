---
"dashboard": minor
"server": minor
---

Add an Employees tab to the AI Insights section that tracks Gram uptake and compliance across organization members. Shows per-member token usage, compliance status, and last activity over the last 30 days, paginated at 25 per page. Usage is attributed by matching the email reported by each AI coding tool (Claude Code, Cursor) to the member's Gram account. Includes a backend refactor to share user-by-email resolution logic across Claude and Cursor hook handlers.
