---
"server": minor
---

Add `userSessionIssuersCimdClients.verifyURL`, which probes a client ID metadata document URL and reports whether it is reachable and spec-compliant without saving anything. Every probe outcome is a successful response distinguishing a malformed URL, an unreachable endpoint, a non-JSON body, and a document that violates the spec, so an operator can fix a URL before adding it rather than discovering the problem when a client fails to authorize. Adding a URL does not fetch the document itself, so configuration never depends on a vendor's host being up; verification is an explicit step taken when it is wanted. The endpoint is rate limited per project, since it is the one place a caller can make Gram fetch a URL of their choosing. The OAuth authorization path is unchanged and still reports these outcomes as a single opaque error, so it cannot be used to probe external hosts.
