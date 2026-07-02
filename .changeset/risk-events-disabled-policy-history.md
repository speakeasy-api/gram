---
"dashboard": patch
"server": patch
---

Risk Events now shows historical findings for turned-off policies. Filtering the Risk Events page by a disabled policy previously returned no results because the query required the policy to be enabled; explicit policy filters now surface a disabled policy's past matches. The dashboard flags the inactive policy (a banner plus an "(inactive)" label in the filter) so it's clear the data is historical. The default unfiltered view is unchanged and still lists only active policies.
