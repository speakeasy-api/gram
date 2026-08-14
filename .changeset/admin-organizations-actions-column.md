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
row being peeked at, and it matches the hover. Hover and the open-menu highlight
are now fully opaque, which they had to become. A translucent highlight was
painted twice on the pinned cell, once by the row and once by the cell stacked
above it, which made it visibly darker than the rest of its row and let the
scrolled columns show through it.

Alt+click a row to peek at it, which the peek panel's first release had and then
lost. Plain click still opens the organization, and the peek button in the
Actions column still peeks, so the gesture is a shortcut rather than the only
way in. It works on any part of the row including the organization's name, whose
link would otherwise treat Alt+click as "save link as" and start a download;
verified in Chromium that the download does not happen. Ctrl, Cmd and Shift are
left alone, so opening a row in a new tab or window still belongs to the link.

The Actions column cannot be hidden from the Columns menu, which would otherwise
put peek and every write out of reach of the whole list.
