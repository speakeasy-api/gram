---
"admin": patch
---

Show a platform admin how much work the organizations list is holding, in a
strip of three figures above the table.

Organizations counts every organization on the platform and says how many were
created in the last seven days. Trials ending in 7 days counts the trials in the
`ending_soon` state. Disabled counts the switched-off organizations and says how
many were switched off in the last seven days.

Each figure is a control. Pressing one filters the table to the rows behind it,
replacing whatever was filtered rather than adding to it, and clearing the
search term the figure never counted.

The figures describe the whole platform, so they do not change when you filter
the table. Disabling an organization, re-enabling one or extending a trial moves
a figure, so each of those refreshes the strip.

Applying a filter set, from a figure or from the filter sheet, now returns to
the first page even where the set applied is the one already on.

The Status control now names the view it is showing, "Active and disabled",
where it used to count it as "2 selected".
