---
"server": patch
"dashboard": patch
---

The activity log can now record and render a restarted enterprise trial. The
entry reads "restarted enterprise trial" and is credited to the Speakeasy team
rather than to the individual operator, which is the label the log already gives
a Speakeasy action taken inside a customer's organization.

Nothing produces the entry yet. The admin action that restarts a trial follows
separately, and this change is the log's half of it: the action name, the writer
that records it, and the phrase the dashboard shows for it.

The collective "Speakeasy Team" label now has one definition instead of two.
The activity log applies it on read, by matching an actor against the members of
the Speakeasy organization. A writer that already knows it is acting as staff
has to apply the same label when it records the entry, because the read-time
mask can only recognise an actor that has a Gram user id, and an operator
authenticated through the admin app does not have one. Both paths now read the
label from the same constant, so one action cannot appear under two different
names depending on which path wrote it.
