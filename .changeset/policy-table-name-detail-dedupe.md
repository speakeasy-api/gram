---
"dashboard": patch
---

Stop the Policy Center table from printing the same thing twice per row. The "Categories / Prompt" column has been folded into the policy column as a second line that only appears when the policy name doesn't already convey it — so "Secrets Exposure Flagger" no longer sits next to "Secrets", and a prompt-based policy whose name is an excerpt of its guardrail shows the name alone. Policies whose name omits a category (or whose guardrail says more than its name) still show the detail, now with the freed-up width.
