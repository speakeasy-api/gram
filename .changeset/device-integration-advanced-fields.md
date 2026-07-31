---
"dashboard": patch
---

Tuck optional device-integration settings behind an "Advanced" disclosure in the connect/configure sheet, so the default view shows only the required fields (e.g. Drata is just Region + API Key + Test — the Custom Connection ID is created automatically). Driven by the descriptor's `required` flag, so it applies to every provider; the section auto-expands when an optional field already holds a value.
