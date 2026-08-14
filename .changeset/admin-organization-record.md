---
"admin": patch
---

An organization in the admin app is now a record rather than a page. The
sidebar drops the global nav while an operator is inside one and shows the
record instead: a row back to all organizations, the organization's name with
its account type and trial state under it, and Overview, Projects and Members
with a count beside each. The breadcrumb above reads Organizations, then the
organization by name, then the view.

Each of those views has its own address, so a link to one organization's
members opens on its members, a refresh stays where it was, and the back button
walks back through the views rather than out of the record. A project opened
from the list stays inside the record it belongs to, and the record's name,
trial and actions stay on screen above it.

The record's name, account type and trial now sit in a header at the top of
every view, beside Open in Gram and Disable. An organization on a live trial
also gets a callout naming the day the trial ends, with Extend trial beside the
date it acts on. An organization that never trialled shows no trial mark at all.

The facts themselves are unchanged, apart from one thing worth knowing before
you read a date: dates in the moved content are now the server's day in UTC with
no clock time, the same reading the organizations list has always shown. The
Members table's Last login and Overview's Updated used to carry a time of day in
the reader's own zone and no longer do.
