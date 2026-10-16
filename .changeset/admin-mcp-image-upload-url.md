---
"server": patch
---

Add a staff Admin MCP tool that returns a single-use signed upload URL for publishing an image as a public Speakeasy asset, for example a registry entry icon. The PUT response gives the asset's public URL. Only PNG, JPEG, GIF and WebP up to 4 MiB are accepted, detected from the file bytes.
