---
"server": patch
---

Store imported AI provider chat messages and titles that contain NUL bytes by dropping the byte, instead of failing the compliance sync on that window.
