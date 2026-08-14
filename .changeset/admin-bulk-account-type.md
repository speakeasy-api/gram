---
"server": patch
---

Platform admins can now set the account type on many organizations in one call.
Until now the only way to move a batch was one request per organization, which
left the operator half-applied if a request in the middle failed. The new admin
endpoint takes a list of organization ids and one account type, writes them in a
single statement, and reports back the ids it wrote and the ids from the request
that matched no organization. A stale id in the paste therefore costs the
operator that one row rather than the whole batch, and the response says which
row it was instead of leaving them to compare lists by hand.

One call carries at most 1000 organization ids. That is far above any selection
an operator can realistically make in the dashboard, and it keeps a mistaken
paste from producing a response that echoes the whole input back.

Both write paths now accept only `free`, `pro` and `enterprise`, matched exactly.
The single-organization update endpoint used to take any string at all, so a
typo or a difference of capitalisation was written straight to the record and
only showed up later as an organization on an account type nothing recognises.
A value outside the list is now refused before anything is written, and the
refusal names the value that was wrong. Existing records holding some other
account type are left alone and keep working until somebody edits them.

The admin dashboard's bulk selection and confirmation step follow.
