---
"admin": patch
---

Add the URL contract that opens the customer-facing dashboard already scoped to a chosen organization, replacing the set-a-cookie-and-log-in-again routine. The slug rides in the `redirect` parameter of the login URL, which the server reads back as the first path segment of the destination; both sides are now pinned by tests so the shape cannot drift, because getting it wrong lands the operator on their own organization with no error. The row action that uses this link follows.
