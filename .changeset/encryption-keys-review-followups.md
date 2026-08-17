---
"dashboard": patch
---

Address review feedback on the organization Encryption Keys page. User-facing
copy now says Speakeasy rather than Gram, matching the branding convention the
dashboard already follows, and External Services and Encryption Keys sit
together directly under API Keys in the Settings nav.

Overview tabs lay short fields out in columns instead of giving each one a
full-width stacked block, which had turned a handful of scalar values into a
long scroll. Long machine values such as a crypto key version path stay full
width, where they read on one line rather than wrapping in a narrow column.

An external credential can now be created from the key form itself. A key is
unusable without one, so an organization's first key previously dead-ended on an
empty picker linking to another page, losing whatever had been filled in.
