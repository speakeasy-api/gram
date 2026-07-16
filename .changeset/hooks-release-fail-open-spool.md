---
"hooks": minor
---

Cut a speakeasy-hooks release containing the DNO-497/DNO-498 client work that merged after hooks@0.1.1 was tagged and therefore never shipped: the org-settings fail-open cache (the binary mirrors the server-confirmed `fail_open` posture next to its credential cache and consults it when no verdict is obtainable, so fail-open orgs actually fail open during control-plane outages), the offline payload spool + drain (unsent events buffer locally and replay on recovery), and the typed replay marker header. Without this release the pinned 0.1.1 binary fails closed for every org during an outage and spools nothing. After the release is published, `hooksBinaryVersion` and the embedded archive checksums in `server/internal/plugins/hooks_bootstrap.go` must be re-pinned in a follow-up.
