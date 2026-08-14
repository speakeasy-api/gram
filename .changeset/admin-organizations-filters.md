---
"admin": patch
---

Let a platform admin narrow the organizations list by account type, trial state
and status, picking as many values in each as they need.

The toolbar carries one control per group. Any of them opens the same sheet, on
the group that was pressed, and each group is a multi-select: Type takes any of
free, pro and enterprise, Trial takes any of the six states the rows show, and
Status takes active, disabled or both. An empty group is a default rather than
an empty result, and each control says its own: all types, all trial states,
active only.

The trial options read from the same map the trial badge on the row renders, so
the filter and the rows it returns cannot say different words for one state.

Nothing reaches the table until the operator applies. Picking three values
otherwise costs three requests and shows two lists nobody asked for on the way
to the one they did. Escape closes the sheet, discards the edit and puts the
keyboard back on the control it opened from. Clear all resets every filter and
leaves the search term alone: the term is not a filter this sheet holds.

The chosen filters live in the URL, so an operator can paste the view they are
looking at and get the same rows back. Applying returns to the first page and
keeps the sort. Values are ordered as the pickers offer them and deduplicated,
so two operators who chose the same filter in a different order send one request
and share one cache entry. An account type from outside the offered list is kept
rather than dropped, because dropping it would widen a link's view while the
control still read "all types".

The request now sends `account_types`, `trial_states` and `disabled_states`, each
repeated once per chosen value, in place of the single `account_type` and the
`include_disabled` flag.
