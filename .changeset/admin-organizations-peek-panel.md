---
"admin": minor
---

Peek at an organization beside the list. Every row in the admin organizations
list carries a peek control that docks a 400px panel next to the table instead
of leaving the page, so the search term, the filters and the scroll position
all survive the look-up. The panel shows account type, trial end, member count,
creation date and both ids, with a copy control beside each id that confirms
with a check. The table narrows to Name, Slug and Type while the panel is open,
and gives the operator's own column choices straight back when it closes.

The control takes the mouse and the keyboard alike. It reports whether its own
panel is open, and it closes the panel it opened. Arrow Down and Arrow Up walk
the panel through the rows on screen and scroll each one into view, and Escape
closes it. Every close puts the keyboard back on the control that opened the
panel. A screen reader is told which organization the panel is showing, each
time the panel opens, moves to another row, or closes.
