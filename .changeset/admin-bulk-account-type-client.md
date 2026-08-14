---
"admin": patch
---

Let a platform admin set one account type on many organizations at once. Ticking
rows in the organizations list turns the strip above the table into a bulk
control: it counts what is ticked, offers the account types the server accepts,
and reads the count and the target type back in a confirmation before anything is
written. Nothing is written until that confirmation is accepted.

The ids come from the ticked rows and from nowhere else. There is no field that
takes an id, because the write matches an id case-sensitively while the list
search matches case-insensitively: an id pasted in the wrong case would come back
as missing while the row it names sat on screen.

An id the server matched no organization to is named on screen after the write,
by the organization the operator ticked, rather than being counted silently as
done. The selection is dropped whenever the rows change under it, so an account
type cannot be set on a record that scrolled out of view.

Row selection is opt-in for the shared admin table. A page that does not ask for
it renders exactly as before.
