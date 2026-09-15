---
"server": patch
---

Shadow AI now detects the current ChatGPT desktop app, which ships under a different bundle id than ChatGPT Classic and so was previously invisible to device scans. The two builds are separate targets: an organization sees which of them a device has installed and running, and can decide about each.

The ChatGPT gateway matchers move from the Classic target onto the new ChatGPT one. Both builds authorize through the same documents, so the gateway cannot tell them apart and a decision recorded against Classic would have reached every ChatGPT caller. An organization that blocked ChatGPT Classic to refuse ChatGPT traffic should record that decision against ChatGPT instead; Classic remains visible on the device but is no longer enforceable on its own.
