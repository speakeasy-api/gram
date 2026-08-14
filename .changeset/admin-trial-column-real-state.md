---
"admin": patch
---

Show an organization's real trial state in the admin dashboard. The
organizations list, the peek panel and the organization detail page all read
`trial_state` and `trial_ends_at` instead of `free_trial_ends_at`, which was
defaulted for every organization and so reported a trial that never happened.

The list column is now headed `Trial` rather than `Trial ends`, and reads as a
state: a badge carrying `Running`, `Ending soon`, `Expired`, `Demoted` or
`Converted`, with `ends` and the date beside it only while the trial is still
live. An organization that never trialled reads as a dash.

The three surfaces render one shared component, so the same organization cannot
read one way in the list and another way on its own page. The old fields stay
on the API for now; a follow-up takes them off the wire.
