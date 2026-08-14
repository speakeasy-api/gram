---
"admin": patch
---

Let a platform admin disable an organization, re-enable it, and extend its
enterprise trial without leaving the organizations list.

Every row carries a menu with those actions, and the peek panel repeats them as
buttons in its footer, so the operator acts on the record they are already
reading. The menu offers Disable or Re-enable, never both: which one it shows
follows the row, so a record disabled a moment ago offers the way back
immediately. Extend trial is offered only for a trial that is running or ending
soon, and never for a disabled organization, whose trial runs on while every
member is locked out.

Disabling asks for a confirmation first, because it takes Gram away from every
member of the organization until it is re-enabled. Re-enabling does not: it
gives access back, and a second press costs a request and nothing else.

Extending asks for a day count, starting at 14. The days are added to the date
the trial ends on now rather than to today, so extending it early does not
shorten it. A count outside 1 to 365 is refused in the dialog, with the reason,
before a request the server would reject leaves the browser.

The list repaints from what the write answered, with no second request behind
it, so the row and the panel show the new state as soon as it lands. A read
already in flight when the operator acts is dropped rather than left to finish,
because it answers with the row as it was and would put that back over the
write.

A write that failed says why. A write made from a dialog reports inside that
dialog. Re-enable has no dialog, so its failure raises a message above the list
that stays until it is dismissed or the next write succeeds. Every outcome also
goes through the polite region the list already announces the peek with, and the
keyboard returns to the control that opened the dialog rather than to the top of
the page.
