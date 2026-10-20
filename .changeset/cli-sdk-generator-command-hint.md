---
"cli": patch
"dashboard": patch
---

Name the Speakeasy SDK generator when one of its commands, such as `speakeasy run`, reaches the AI Control Plane CLI. Both CLIs install a binary named `speakeasy`, so the error now says which CLI is on `PATH` and how to install the SDK generator as `speakeasy-sdks` next to it. The Functions getting-started steps and the CLI and Functions READMEs now check the install with `speakeasy --control-plane-cli`.
