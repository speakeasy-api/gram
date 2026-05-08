---
"dashboard": patch
---

Fix a crash in the RBAC dev toolbar when toggling `environment:read` or
`environment:write` for the first time. The toggle handler spread `undefined`
into a new state entry, producing an object without the `resources` field;
the next render then crashed reading `.length` on undefined. Hardened
`toggleScope` and `setScopeResources` to materialize a known-good baseline
before spreading, and added a defensive `!= null` at the render site so any
legacy malformed localStorage state can't crash either.
