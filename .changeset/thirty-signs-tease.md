---
"server": patch
---

Updated oops.ErrHandle to include panic recovery. There are a few HTTP handlers
included in some services (alongside Goa endpoints) that needed this protection.
The log messages will also include stack traces for easier debugging.
