---
"admin": patch
---

Show an organization's real trial state in the admin dashboard. The
organizations list, the peek panel and the organization detail page all read
`trial_state` and `trial_ends_at`, which the server derives from the `trials`
table, instead of `free_trial_ends_at`.

That column is `NOT NULL` with a default of signup plus fourteen days and no
application code ever writes it, so every organization reported a trial end
date whether or not it ever trialled, and an operator who trusted the date
acted on the wrong account. The list column is now headed `Trial` rather than
`Trial ends`, and reads as a state: a badge carrying `Running`, `Ending soon`,
`Expired`, `Demoted` or `Converted`, with the end date beside it only while the
trial is still live. An organization that never trialled reads as a dash.

The three surfaces render one shared component, so the same organization cannot
read one way in the list and another way on its own page. The old fields stay
on the API for now; a follow-up takes them off the wire.
