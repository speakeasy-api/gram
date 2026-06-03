---
"dashboard": patch
---

Fix inconsistent results in the filter dropdowns on the Insights and Logs pages (e.g. "Filter by user email"). `MultiSelect` filtered its own option list while cmdk's built-in filter was also enabled, so the two raced as the user typed: valid matches could fall into the "No results found" empty state and it could stay stuck when characters were deleted. The component's own filter is now the single source of truth.
