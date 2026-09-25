---
"dashboard": patch
"server": patch
---

Admitting a workload is now a single field. Neither dialog asks about wildcards: registering an issuer no longer offers a permission switch, and admitting a subject no longer offers a Match control. A subject ending in `*` is a wildcard rule, anything else is exact, and the dialog states what a wildcard would admit — which subjects, and the agent each would inherit — at the point it is written. Lossless, because an exact subject may never contain a `*`, so there is no value this could misread. Where an issuer has wildcard admission turned off, a rule stating one is refused with the reason under the field.
