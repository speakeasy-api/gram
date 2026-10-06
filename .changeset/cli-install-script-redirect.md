---
"dashboard": patch
---

`https://ai.speakeasy.com/cli.sh` and `/cli.ps1` now redirect to the CLI install scripts, so `curl -fsSL https://ai.speakeasy.com/cli.sh | bash` works. Before, those paths returned the dashboard page.
