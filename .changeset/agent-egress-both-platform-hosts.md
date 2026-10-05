---
"dashboard": patch
---

Agent network allowlists now list both platform hosts, `app.getgram.ai` and `ai.speakeasy.com` (`dev.getgram.ai` and `dev.ai.speakeasy.com` on dev), on whichever host you open the setup page from. Published plugins send to the server URL from their last publish, so an allowlist with both hosts keeps working when the server URL changes and plugins republish.
