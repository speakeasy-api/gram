# @gram/functions

## 0.0.3

### Patch Changes

- ce519d2: updates the Gram Functions web server to set a `Gram-Invoke-ID` header containing the decrypted invocation ID from the authorization bearer token. By including this ID in the response, we can add an extra layer of defense in Gram that asserts a function call was handled by a server holding the auth secret.
- 19310ba: Add missing support for functions.ts files in Gram functions

## 0.0.2

### Patch Changes

- 3001e53: Fix the entrypoint script for Gram Functions runner images to correctly invoke the desired command with its arguments.

## 0.0.1

### Patch Changes

- caee968: Harden function runner images by:
  - Add basic safety checks after image builds to screen out setuid/setgid and check fs permissions.
  - Remove most unnecessary system binaries
  - Using root to bootstrap the filesystem of the runner then starting the runner as a non-root user. This ensures code is tamper proof. Alpine's `exec-su` is used to drop privileges.
  - Moving from `/srv/app` to `/var/task` as the working directory to following aws lambda conventions and making all created files and directories owned by root and read-only.
  - Allowing only a minimal set of headers to be sent from functions and especially preventing any headers related flyio's replay feature [^1].
  - Removing debug symbols and trimming paths from the binary to reduce size and have stable paths in stack traces.
  - Setting build info in the binary and implementing a flag, `gram-runner -version` to print it. We also set the version in a `Gram-Runner-Version` header on all outgoing responses.

  [^1]: https://fly.io/docs/networking/dynamic-request-routing/
