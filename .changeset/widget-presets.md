---
"server": minor
"dashboard": patch
---

Product pages can declare their widgets in code. A preset is a page's rows of widgets on a 12-column grid. Presets are checked into the repository, served by `widgets.getPreset`, and validated against the catalog in CI, the same check a saved widget gets. A catalog change that breaks a page fails the build. Staff can compare each card with the legacy figure it replaces while a page is cut over.
