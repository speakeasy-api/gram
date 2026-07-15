---
"dashboard": patch
---

TimeRangePicker: natural-language input that the AI parser normalizes to a preset (e.g. "last week" → 7d) now applies as the preset's concrete date range when that preset isn't offered via `availablePresets`, instead of silently doing nothing. Fixes typed date ranges on the billing page, which passes `availablePresets={[]}`.
