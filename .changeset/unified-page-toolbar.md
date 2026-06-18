---
"dashboard": patch
---

Unify the search, filters, sort, and view-as controls on every list page into a single `Page.Toolbar` component, replacing the per-page mix of bespoke search boxes, filter sidebars, sort dropdowns, and view toggles.

- One contained control bar per page (search + filters on the left, sort + count + view on the right), with uniform control heights.
- Filter chips and sheet share a typed schema (`defineFilters`/`useFilterState`); adds Status + Source filters to the MCP page.
- Folds in fixes: "Reset to default" now clears every filter atomically, the date picker opens inside the filter sheet, and empty filter labels are pluralized ("All servers").
