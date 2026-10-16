---
"server": patch
---

Add a staff Admin MCP tool that returns an upload URL and a single-use upload token, sent as an Authorization header, for publishing an image as a public Speakeasy asset, for example a registry entry icon. The PUT response gives the asset's public URL. Only PNG, JPEG, GIF and WebP up to 4 MiB are accepted, detected from the file bytes.
