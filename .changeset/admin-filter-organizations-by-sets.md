---
"admin": patch
---

The admin organizations list now filters on sets rather than one value at a time: `account_types` takes several account types, `trial_states` takes any of running, ending soon, expired, demoted, converted or none, and `disabled_states` takes active, disabled or both. An empty set means no filter, except `disabled_states`, where an empty set still means active only. A value outside the known list matches nothing rather than failing the request, so an operator pasting a colleague's URL gets an empty table instead of an error. The total reported alongside the page counts the same filtered set, trial state included.

This is an expand step. The single-valued `account_type` and the `include_disabled` boolean both still work: `account_type` joins `account_types` as one more member of the same set, and `disabled_states` overrides `include_disabled` when supplied. Keeping them lets the server ship before the dashboard changes. Retiring them is a separate contraction step.
