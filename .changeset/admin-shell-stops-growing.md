---
"admin": patch
---

Scroll a long list in the admin app and the table moves, not the page. The
search box, the filters and the pagination row hold their places while the rows
travel under them, and the sidebar and the page header stay on screen
throughout.

Until now a list longer than the window grew the whole page instead. Reaching
the fiftieth organization carried the search controls off the top of the screen
and left the pagination row somewhere below the fold, so an operator who wanted
the next page, or wanted to change a filter after reading the rows, had to
scroll the document back up to find the controls again.

Every page in the admin app gains the behaviour. Anything taller than the window
now scrolls within the layout, and the shell around it stays put.
