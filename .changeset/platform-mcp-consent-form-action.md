---
"server": patch
---

Fix the Platform MCP consent page blocking the redirect back to ChatGPT after "Give access". Chrome checks the page's `form-action` policy on every redirect after the form posts, and ChatGPT's callback redirects onward to another origin, so the browser blocked the navigation. The page no longer sets `form-action`, matching the MCP consent page.
