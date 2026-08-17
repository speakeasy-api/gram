---
"admin": patch
---

Platform admins can now create an organization from the organizations list. The
endpoint shipped without a caller, so opening an organization still meant leaving
Gram for the WorkOS dashboard. A Create organization button sits at the end of
the toolbar, takes a name, and creates the organization.

What it creates is deliberately plain: no members, not whitelisted, no trial, and
on the free tier. The server validates the name exactly as self-serve signup
validates it, so an operator cannot name an organization something a customer
could not have named it.

The list is fetched again after a create rather than having the new row written
into the page on screen. Where that row belongs depends on the sort, the filters
and the page the operator is on, so putting it anywhere by hand would put it
somewhere the server would not have. For the same reason the confirmation names
the organization that was created and says the list may not show it: a list
filtered to running trials is right to leave a new free-tier organization out,
and the operator still needs to be told the write landed.

A refused name leaves the dialog open with the name still in it and the server's
reason beside it, because a rejected name is one the operator wants to edit
rather than retype. A deployment with no WorkOS configuration refuses this way
too, so it reports that it cannot create organizations instead of failing
silently. A refusal also leaves the list as it found it, so pressing Create while
the list is still loading cannot leave the table saying there are no
organizations. While a create is in flight, the confirm button, the cancel button, the
close control and the Escape key are all held, so one press creates one
organization.
