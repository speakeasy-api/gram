---
"admin": patch
---

Gather the organizations list's two row controls into one Actions column, pinned
to the right edge of the table. Peek and the row menu used to sit in two
separate columns ahead of the name, which put two controls between the operator
and the thing they came to read, and left the row's first column carrying
nothing they could name. They are now one cell at the end of the row, peek
first, sized to its contents so it does not read as an empty gutter.

Pinned rather than merely last, because the list is wider than most windows. A
trailing column that scrolled with the rest would start life off the right edge:
in a 760 pixel wide window it sat 227 pixels past the edge of the viewport, so
neither control could be seen or clicked until the operator scrolled the table
sideways. Pinned, both are on screen from the start and hold their position
while the columns scroll underneath them.

The pinned cell takes its colour from the row it belongs to rather than a flat
one of its own, so it stays invisible as a cell: it matches the highlight on the
row being peeked at, and it matches the hover. To give it something opaque to
inherit, rows in the organizations list now carry a background of their own, and
their hover and open-menu highlights became fully opaque. A translucent
highlight was painted twice on the pinned cell, once by the row and once by the
cell stacked above it, which made it visibly darker than the rest of its row and
let the scrolled columns show through it. Only this list changes; every other
admin table keeps the highlights it had.

Alt+click a row to peek at it, which the peek panel's first release had and then
lost. Plain click still opens the organization, and the peek button in the
Actions column still peeks, so the gesture is a shortcut rather than the only
way in. It works on any part of the row including the organization's name, whose
link would otherwise treat Alt+click as "save link as" and start a download;
verified in Chromium that the download does not happen. Holding Alt together
with Ctrl, Cmd or Shift does not peek, but it still cancels that download rather
than leaving the operator with an HTML file they did not ask for.

Ctrl, Cmd and Shift without Alt still belong to the link, so opening an
organization in a new tab or window works as it did. Used anywhere else in the
row they now do nothing. Until now they fell through and navigated the list away
in the current tab, which took the list out from under the tab the operator was
opening.

The Actions column cannot be hidden from the Columns menu, which would otherwise
put peek and every write out of reach of the whole list.
