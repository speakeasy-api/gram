---
"server": patch
---

Tool Logs searches and custom attribute filters are faster. The slow path no longer converts every log's full attributes to text, and it sorts the logs once instead of twice.
