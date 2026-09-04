---
"server": patch
---

Store imported AI provider chat messages and titles that contain NUL bytes by replacing the byte with U+FFFD, instead of failing the compliance sync on that window.
