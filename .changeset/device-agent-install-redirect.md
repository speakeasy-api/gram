---
"server": minor
---

Add `GET /v1/install/device-agent-macos.pkg`, a stable redirect to the current signed macOS device-agent installer. Resolves the current version from the public device-agent releases manifest server-side and 302s to the versioned pkg, so docs and IT-admin instructions can link to one URL instead of hardcoding a version that goes stale every release.
