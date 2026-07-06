---
"@gram-ai/elements": patch
---

TimeRangePicker: clicking outside while the input is in edit mode now exits editing and closes the dropdown in a single click (previously the first click only blurred the input, and typed-but-uncommitted text made the dropdown undismissable by clicking). Uncommitted input text is discarded on outside click.
