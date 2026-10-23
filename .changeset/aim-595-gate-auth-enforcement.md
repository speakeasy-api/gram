---
"server": patch
"admin": patch
---

Staff can now turn on upstream auth enforcement per organization from the upstream OAuth migration page, and only once its auth clients, credential choices and assistant profiles are recorded. Under enforcement each request is served with the auth client its credential chose, assistant turns with the one their profile names, and using that client requires permission; a credential with no usable choice is asked to reconnect rather than served with another client. Valid credentials from another organization are refused on an organization's public servers instead of being served anonymously. Turning enforcement off returns the organization to the previous behavior within a minute.
