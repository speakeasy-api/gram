---
"admin": patch
---

The admin organizations and organization detail endpoints now report each organization's real trial state and end date, read from the trials table. The state separates a running trial from one that is ending soon, expired, demoted or converted, and from an organization that never trialled. The existing free trial fields default to fourteen days after signup for every organization, so they make every row look like it is trialling. Those fields still ship unchanged, and the admin dashboard starts reading the new ones in a follow-up.
