---
"admin": patch
---

Fix two ways the organizations list left a keyboard operator without an answer
while the peek panel was open.

The panel's arrow keys now reach only the panel itself and the peek control of
the row it is showing. Every other control under the list keeps its own keys
back: Arrow Down on the pager, or on a button inside the panel body, scrolls the
way it does anywhere else instead of walking the panel to a record the operator
is not looking at and swallowing the scroll on the way.

Escape is scoped wider, because it has no scrolling of its own to lose. It
closes the panel from anywhere inside it, the buttons in its body included, and
puts the keyboard back on the peek control of the row that was open. It still
does nothing on the pager, and a tooltip or menu opened from inside the panel
still takes the first Escape for itself.

The Columns control also works again while the panel is open. Checking one of
the columns the panel hides used to do nothing at all: the checkbox snapped
back with no column, no refusal and nothing said. It now closes the panel and
shows the column in the same moment, and says so, because asking for a column
is asking to see it. A column the panel was not hiding leaves the panel where
it is, and the keyboard stays in the Columns menu either way.
