---
"dashboard": patch
---

Fix the dashboard's `resourceKindForScope` so `workload:` scopes derive the `workload` resource kind instead of falling through to `*`. Without it the check selector carried `resource_kind: "*"` while the grant carried `resource_kind: "workload"`, and `selectorMatches` requires the grant value to be the wildcard — so a holder with correct `workload:read` grants was refused and the Workload Identities page rendered Access Restricted.
