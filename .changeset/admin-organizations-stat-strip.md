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
Organizations opens the whole platform, Trials ending in 7 days applies
`trial=ending_soon`, and Disabled applies `disabled=disabled`. A press replaces
whatever was filtered rather than adding to it, so pressing Disabled while a
trial filter is set leaves only the disabled filter, and the filter sheet
follows. The search term is not one of the three filters and is left alone.

The first two cells set the status filter to both statuses rather than clearing
it, because the table shows active organizations only when nothing is chosen.
Without that, the figure and the list it opened would differ by exactly the
number of disabled organizations.

The middle cell counts the same `ending_soon` state that the filter it applies
sends, so the figure cannot disagree with the rows it navigates to.

The figures describe the whole platform and do not change when you filter the
table.

Disabling an organization, re-enabling one or extending a trial moves a figure,
so each of those refreshes the strip.
