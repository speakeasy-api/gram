---
"server": patch
---

Tag the assistant runtime image with a content hash so deploys that don't change the runtime image sources reuse the existing fly machines instead of recycling them on every commit.
