---
"admin": patch
---

Fix several ways the organizations list left a keyboard operator without an
answer while the peek panel was open.

The panel is now a tab stop with a visible focus ring. Arrow Up and Arrow Down
on it walk the peek from record to record, and before this the only way to reach
it was the focus it took when it opened: one Tab to the close button and record
navigation was gone until the operator went back to the row's peek control.

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

The Columns control also works again while the panel is open. The panel
overrides six columns: it hides five, and it forces Name on. A menu click on
any of those used to do nothing an operator could see, because the panel's
override outranked it. Checking one the panel hides snapped the checkbox back
with no column and nothing said, and unchecking Name was worse: that one looked
like nothing had happened, then took the Name column away later, when the panel
closed.

Toggling any of the six now closes the panel and applies the change in the
same moment, and says which way it went, because toggling a column is asking
for that column. A column the panel does not override leaves the panel where it
is, and the keyboard stays in the Columns menu either way.
