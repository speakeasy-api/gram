---
"dashboard": minor
---

Show the Okta application inventory in the identity provider setup card, read-only. The step opens as soon as the Okta connection is live and reads the tenant live on arrival, so there is nothing to click first and nothing is stored. The cards carry each application's Okta-hosted logo, the host it signs on at, its status, its sign-on mode, and how many groups and people Okta assigns it to, with search by name, an All / Active / Inactive filter and a result count. Above it, a line saying how many applications were read and when, alongside the server's own account of the read, which is what makes a missing assignment count legible: the count is omitted on a large tenant and dropped where the read failed, so a dash means not known rather than none. Refresh re-reads Okta. Nothing here proposes, selects, or changes anything.
