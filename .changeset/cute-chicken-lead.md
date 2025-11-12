---
"server": patch
---

Added the necessary Authorization header to the Fly API delete machine request
to ensure proper authentication. We also increase the reap batch size to 50.
