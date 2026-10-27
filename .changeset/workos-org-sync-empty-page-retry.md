---
"server": patch
---

Organization changes from WorkOS, such as membership role updates, are now applied within about a minute even when WorkOS sends its webhook before the event is available to read. Previously the change could wait until the organization's next WorkOS event.
