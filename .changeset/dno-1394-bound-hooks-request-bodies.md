---
"server": patch
---

Hook endpoints now reject request bodies larger than 20 MiB, both on the wire and after gzip decompression, with a 413 before the body is parsed or the caller is authenticated. Skill content uploads keep their stricter 512 KiB limit and now also answer oversized bodies with 413 instead of 400.
