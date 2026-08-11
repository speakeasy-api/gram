---
"server": minor
---

Assistant runtimes can now run locally: the new `local` runtime provider (the
local-development default) starts one Docker container per assistant on demand,
reuses it across turns, and automatically replaces idle containers when the
runtime image is rebuilt — no Fly.io credentials or registry pushes needed for
local image development.
