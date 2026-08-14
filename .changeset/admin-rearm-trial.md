---
"server": patch
"dashboard": patch
---

Platform admins can now put a demoted enterprise trial back on. Until now the
expiry sweeper's demotion was one-way: it dropped the organization to the free
tier, put it back behind the book-a-demo gate and switched off its model
provider keys. Only the keys could be undone, one at a time, through the
existing admin action for enabling a key; the tier and the gate had no undo at
all. An operator who wanted to give a customer a second run had no way to do it,
and extending the trial was not the same thing, because an extension moves an
end date and leaves the free tier exactly where the demotion left it.

Re-arming restores all of it at once: the account type the trial grants, the
whitelist flag, every model provider key the demotion switched off, and a fresh
run of the length the operator asks for, capped at a year and counted from now. The end date is
counted from now rather than added to the old one on purpose, because a demoted
trial's end date is already in the past and adding to it could land in the past
again, which would leave the sweeper free to demote the organization a second
time within the hour.

One caveat on the keys: this is the deployed behaviour. A local development
stack has no OpenRouter account behind it, and its stand-in client accepts the
refresh without doing anything, so a re-arm there reports success and restores
the tier while both key rows stay switched off. Enabling a key locally has the
same gap.

The keys come back up before any of the database changes are committed. That
ordering is the opposite of the demotion's, and it is deliberate: if the model
provider refuses, the organization stays demoted and on the free tier, and the
operator can retry. Any key that came back up before the refusal stays up, which
is what makes the retry cheap. The alternative would advertise a running trial
to a customer whose keys were still switched off.

Only a demoted trial can be re-armed. A trial that is already running is
rejected, so re-arm cannot be used as an extend that ignores the extension
rules. An organization id that matches nothing is reported as not found, the
same answer the disable, enable and extend actions already give, so a mistyped
id does not send an operator off to inspect a trial that was never the problem.

The activity log reads the new entry as "restarted enterprise trial", credited
to the Speakeasy team rather than to the operator who ran it, which is the same
label the log already gives a Speakeasy action inside a customer's
organization. The admin dashboard row action follows.
