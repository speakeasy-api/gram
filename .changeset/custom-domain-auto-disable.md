---
"server": minor
"dashboard": patch
---

Custom domains that stay unhealthy for over a week (7+ consecutive failed daily checks) are now automatically disabled: their routing and TLS certificate are removed, and the dashboard explains what went wrong and walks admins through fixing the issue and reverifying the domain. Gram-side check failures never count toward disabling.
