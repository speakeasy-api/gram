---
"server": patch
---

Platform admins can now create an organization without leaving the admin app.
Until now the only way to open one was the WorkOS dashboard, followed by a wait
for the event sync to notice it, so setting up a customer meant working in two
tools and having no way to tell which step had actually landed. A new admin
endpoint creates the organization in WorkOS and in Gram in one call and reports
it back, ready for the existing list, detail and update endpoints.

The organization it creates is deliberately plain: no members, not whitelisted,
no trial, and on the free tier. Each of those is a separate decision with its
own endpoint, and creating an organization is not a way to make any of them.

The new organization is safe to create even while WorkOS is delivering the event
for it. Both writers derive the Gram organization id from the WorkOS one, so the
admin write and the event sync land on the same row whichever gets there first,
and an organization the sync already created keeps the slug it was given rather
than being renamed. If WorkOS refuses the create, nothing is stored in Gram, and
the operator gets an error naming WorkOS rather than a half-made organization.

Names are validated exactly as self-serve signup validates them, because both
paths now run the same validator: an organization created by an operator cannot
be named something a customer could not have named it.

The local development identity provider gained the two organization routes this
flow needs, so the whole thing can be exercised without a WorkOS account.

The admin dashboard button follows.
