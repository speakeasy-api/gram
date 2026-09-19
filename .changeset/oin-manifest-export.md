---
"server": minor
"dashboard": patch
---

Export the OIN Cross App Access manifest for platform admins: a session-gated `oinManifest.export` RPC that renders the global ID-JAG issuer and client catalog as JSON or Markdown, and a Platform Admin page that previews it, lists blockers, and downloads both files. The export distinguishes the ID-JAG audience (the resource authorization server issuer) from the protected API resource, reports missing resource identifiers without guessing, and enforces the OIN Wizard's 100-registration limit. Catalog readiness is separate from the passing conformance evidence required for submission.
