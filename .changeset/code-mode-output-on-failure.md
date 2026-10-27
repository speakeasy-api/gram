---
"server": patch
---

Code Mode runs that fail now return the output the program printed before the failure, so `print` can be used to debug a traceback, a CPU, memory, result or callback limit, or the wall deadline. Output keeps the same 16 KiB limit and sets `output_truncated` when cut.
