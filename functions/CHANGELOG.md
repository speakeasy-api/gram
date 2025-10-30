# function-runners

## 0.1.3

### Patch Changes

- eccc1bb: Fixed an issue where certain allowed headers in Gram Functions response were interfering with the Trailers that were being set containing resource usage metrics. By removing the Content-Length header from the response, we ensure that the Trailers can be set and read correctly by the client. Previously, setting both headers would prevent response body bytes from being sent back to the server.
- 36f36cb: Updated the runner to detect if the default export from customer TS/JS code is a
  `Promise` to an object containing `handleToolCall` / `handleResources` and
  awaits it before proceeding with a tool/resource request.

## 0.1.2

### Patch Changes

- 329264e: Bind `handleToolCall` and `handleResources` to their owning objects if needed
  in TypeScript runner entrypoint.

  When `handleToolCall` and `handleResources` are exported by an object, ensure
  they are bound to that object so that any references to `this` inside the
  function work correctly. This was breaking the Gram TS SDK which does this:

  ```
  const gram = new Gram()
    .tool(/* ... */);

  // We were calling gram.handleToolCall without binding it to `gram` in
  // gram-start.mjs
  export default gram;
  ```

- a19db7c: Updated function runner images to install the pre-bundled ca-certificates
  package which allows sub-processes to verify TLS connections.
- 329264e: Remove invalid flush option on named pipe in TypeScript function runner
  entrypoint. Pipes are in-memory "files" and do not support flush operations. In
  production, we were observing errors when trying to flush a named pipe:

  ```
  Error: EINVAL: invalid argument, fsync
  ```

## 0.1.1

### Patch Changes

- 6a2eecf: Sets up the ability to track gram functions memory and cpu usage per tool call coming from the function runner

## 0.1.0

### Minor Changes

- 7cd9b62: Rename packages in changelogs, git tags and github releases

## 0.0.4

### Patch Changes

- 468b341: Modifies the functions runner and JS entrypoint to accept `handleResources` entrypoint. Can differentiate between tools, resources, and potential other future entrypoints by type argument.

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
