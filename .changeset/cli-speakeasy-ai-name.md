---
"cli": minor
"dashboard": patch
"@speakeasy-api/functions": patch
"@gram-ai/create-function": patch
---

Install the AI Control Plane CLI as `speakeasy-ai` instead of `speakeasy`, the name the Speakeasy SDK generator uses, so both CLIs can be on `PATH`. The Homebrew `cli` formula, the npm package and the install scripts now install `speakeasy-ai`, and the Homebrew formula no longer conflicts with the SDK generator formula. Releases still publish the `speakeasy` archives for older installs, and a copy run as `speakeasy` prints a deprecation notice. The Functions SDK looks for `speakeasy-ai` first. The dashboard, READMEs and Functions templates use `speakeasy-ai`.
