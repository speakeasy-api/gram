# admin

## 0.1.2

### Patch Changes

- 08414c0: Add the URL contract that opens the customer-facing dashboard already scoped to a chosen organization, replacing the set-a-cookie-and-log-in-again routine. The slug rides in the `redirect` parameter of the login URL, which the server reads back as the first path segment of the destination; both sides are now pinned by tests so the shape cannot drift, because getting it wrong lands the operator on their own organization with no error. The row action that uses this link follows.
- cfc9efd: The admin organizations and organization detail endpoints now report each organization's real trial state and end date, read from the trials table. The state separates a running trial from one that is ending soon, expired, demoted or converted, and from an organization that never trialled. The existing free trial fields default to fourteen days after signup for every organization, so they make every row look like it is trialling. Those fields still ship unchanged, and the admin dashboard starts reading the new ones in a follow-up.

## 0.1.1

### Patch Changes

- 11f6e49: Keep the organizations list's search and filters in the URL, so an operator can paste the view they are looking at to a colleague and get the same rows back. The table gains a persistent toolbar row with a Columns control, and rows return to the standard density.
