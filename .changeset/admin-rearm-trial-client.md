---
"admin": patch
---

Let a platform admin put a demoted enterprise trial back on from the
organizations list. The endpoint has been there since the demotion sweeper
shipped, and nothing in the admin app called it, so a demotion was one-way from
the admin app and undoing one meant a hand-written API call.

Re-arm trial sits beside Disable and Extend trial, in the row menu and in the
peek panel footer, and it is offered on a demoted trial and on nothing else. A
trial that has converted or is still running is refused by the server, an
expired one has not been demoted yet, and Extend trial covers the trials that
are running: the two actions are never offered on the same record.

The confirmation says what the action does rather than what its day count
suggests. Re-arming restores the organization's account type, brings its model
provider keys back, takes it out from behind the book-a-demo gate, and starts a
fresh trial of the given length counted from now, not from the date the old one
ended on. That is a different action from extending, which only adds days to an
end date.

The day count starts at fourteen days, the trial length the rest of the system
assumes, and a count the server would refuse is refused in the browser instead
of being sent. A request the server rejects leaves the dialog open with the day
count intact, so the operator adjusts the attempt rather than retyping it. The
row repaints from the answer, so an organization does not move out from under
the operator who just acted on it.
