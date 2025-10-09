---
"@gram/functions": patch
---

Harden function runner images by:

- Remove most unnecessary system binaries
- Using root to bootstrap the filesystem of the runner then starting the runner as a non-root user. This ensures code is tamper proof. Alpine's `exec-su` is used to drop privileges.
- Moving from `/srv/app` to `/var/task` as the working directory to following aws lambda conventions and making all created files and directories owned by root and read-only.
- Allowing only a minimal set of headers to be sent from functions and especially preventing any headers related flyio's replay feature [^1].
- Removing debug symbols and trimming paths from the binary to reduce size and have stable paths in stack traces.
- Setting build info in the binary and implementing a flag, `gram-runner -version` to print it. We also set the version in a `Gram-Runner-Version` header on all outgoing responses.

[^1]: https://fly.io/docs/networking/dynamic-request-routing/
