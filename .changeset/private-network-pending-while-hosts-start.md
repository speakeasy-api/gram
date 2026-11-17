---
"server": patch
"dashboard": patch
---

A new private network connection now shows as pending, not as an error, while its gateway hosts are still coming up: while the first HTTPS certificate request is in flight, and for the first minute while the tailnet auto-approves the hosts. If a certificate request is refused, the Domains page now shows the reason the gateway reported, such as a Let's Encrypt rate limit and when the gateway retries, instead of always blaming the tailnet's weekly certificate allowance.
