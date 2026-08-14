---
"server": patch
---

Platform admins can now set the account type on many organizations in one call.
The new admin endpoint takes a list of organization ids and one account type,
writes them in a single statement, and reports back the ids it wrote and the ids
from the request that matched no organization. A stale id therefore costs that
one row rather than the whole batch. One call carries at most 1000 ids.

Both admin write paths now accept only `free`, `pro` and `enterprise`, matched
exactly. The single-organization update endpoint used to take any string, so a
typo or a difference of capitalisation was written straight to the record. A
value outside the list is now refused before anything is written, and the refusal
names it. Existing records holding some other account type are left alone.

The admin dashboard's bulk selection and confirmation step follow.
