---
"dashboard": patch
---

Send a robots noindex signal for the whole dashboard host: the app shell now carries `<meta name="robots" content="noindex, nofollow">` and nginx adds a matching `X-Robots-Tag` header (including on error responses). Nothing on the host wants search indexing — the app is behind login and `/shared/*` pages are tokenized public share links.
