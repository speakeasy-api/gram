---
"dashboard": patch
---

Fix the shared `Link` component so external links render inline with surrounding text. The external-link icon was wrapped in a block-level flex container, which stretched inline links to full width and pushed trailing punctuation to a new line; the icon now sits inline on the text baseline.
