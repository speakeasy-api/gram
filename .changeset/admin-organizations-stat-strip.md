---
"admin": patch
---

Show a platform admin how much work the organizations list is holding, in a
strip of three figures above the table.

Organizations counts every organization on the platform and says how many were
created in the last seven days. Trials ending in 7 days counts the trials in the
`ending_soon` state. Disabled counts the switched-off organizations and says how
many were switched off in the last seven days.

Each figure is a control. Pressing one filters the table to the rows behind it:
Organizations clears every filter, Trials ending in 7 days applies
`trial=ending_soon`, and Disabled applies `disabled=disabled`. A press replaces
whatever was filtered rather than adding to it, so pressing Disabled while a
trial filter is set leaves only the disabled filter, and the filter sheet
follows. The search term is not one of the three filters and is left alone.

The middle cell counts the same `ending_soon` state that the filter it applies
sends, so the figure cannot disagree with the rows it navigates to.

The figures describe the whole platform and deliberately ignore the table's own
filters: they are fetched with no parameters, from a cache entry that has no
key to move, so filtering the table cannot make the strip report the rows
already on screen.

Fed by a new `GET /admin/organizations.stats` endpoint.
