---
"admin": patch
---

An organization record's Overview now reads as three named groups rather than
one flat list of facts. Identity carries the name, slug, organization id, WorkOS
id and the created and updated dates. Plan carries the account type and the
trial. Access carries Whitelisted and Disabled at.

Whitelisted keeps its name and sits on its own, because it gates the
organization's access to the platform rather than expressing a preference.

The slug, the organization id and the WorkOS id can each be copied with one
click, the same control the organizations list already uses when you peek at a
record. An organization with no WorkOS id gets no copy button rather than a
button that copies a dash.

The Members row is gone from the Overview. The record's own nav already counts
members, so the row said the same thing twice.
