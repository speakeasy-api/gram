---
"admin": patch
---

An organization record's editable facts are now committed one at a time. The
save bar that sat under the whole record is gone, along with the draft it wrote
into, so the record no longer holds an unsaved change.

Changing the account type, or the Whitelisted switch, raises a confirmation
naming that one change and nothing else: "Account type: pro → enterprise." The
write that follows carries only the field that was changed. A change that is
not confirmed is never written, and the control goes back to reading the record
on its own.

Both controls are held while a write is in flight, so one record cannot take
two writes at once. Every write now reports through the record's own reporter:
spoken on success, and both spoken and shown on failure, in place of the error
line the save bar used to carry.
