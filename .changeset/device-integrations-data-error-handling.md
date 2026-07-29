---
"server": patch
---

Device integration syncs now record database rejections of vendor-supplied
row content (for example a device record whose name carries a Unicode NUL
escape that jsonb refuses) as visible, backed-off schedule failures instead
of retrying them as infrastructure errors, and URL-kind integration settings
are syntax-checked at save time.
