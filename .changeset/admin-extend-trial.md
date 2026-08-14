---
"server": patch
---

Platform admins can now give a customer more time on a running enterprise trial.
Until now the trial end date was written once, when the trial was granted, and
nothing anywhere could move it, so an operator who wanted to buy a customer
another two weeks had no way to do it.

The extension is added to the trial's current end date rather than to today, so
"another two weeks" means two weeks on top of whatever the customer has left,
and extending a trial that still has three weeks to run cannot accidentally
shorten it. Extensions accumulate, and a single one is capped at a year.

Only a running trial can be extended. A trial that has already converted to a
contract, one that has been demoted, and one whose date has passed but that the
expiry sweeper has not reached yet are all rejected with an error that leaves the
date where it was, rather than quietly re-arming a trial that has already ended.
An organization id that matches nothing is reported as not found, the same answer
the disable and enable actions already give, so one mistyped id does not send an
operator off to inspect a trial that was never the problem.
Extending a trial changes the end date and nothing else: the organization's
account type, its whitelist flag and the record of when the trial began all stay
as they were.

The admin dashboard row action follows.
