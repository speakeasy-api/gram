---
"function-runners": patch
---

Remove invalid flush option on named pipe in TypeScript function runner
entrypoint. Pipes are in-memory "files" and do not support flush operations. In
production, we were observing errors when trying to flush a named pipe:

```
Error: EINVAL: invalid argument, fsync
```
