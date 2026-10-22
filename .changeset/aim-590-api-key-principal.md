---
"server": patch
---

API keys are recognized as their own access principal, `api_key:<id>`, holding only grants written to them, which always match strictly and never include the key creator's access. Server access settings neither list nor replace a key's grants. A new check evaluates server and auth client access for every caller, including API keys and callers without a session, and denies when there is no policy or the request falls outside the key's project or scopes. Nothing calls it yet.
