---
"server": patch
---

Assistants can now update their own triggers. Previously, calling `configure_trigger` on an existing trigger returned a generic internal error every time, even though the assistant could read its triggers fine — its scoped tool was being silently swapped for a stricter variant that demanded fields the assistant isn't allowed to send. As a side effect, an assistant's trigger list no longer leaks sibling assistants' triggers in the same project.
