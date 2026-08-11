# Gram Assistant Runtime Image

The container image assistant runtimes run in: the Rust runner
(`agents/runner`), the bun-based sandbox, and a lightpanda browser.

## Local development

Local development uses the `local` assistant runtime provider (the default,
`GRAM_ASSISTANT_RUNTIME_PROVIDER=local`): the Gram server starts one runtime
container per assistant on your machine's Docker daemon, on demand. No Fly.io
credentials, apps, or registry pushes are involved.

### Workflow

1. Build the image:

   ```sh
   mise run build:assistants-runtime-image
   ```

2. Start Gram normally (e.g. `./zero --agent`).

3. Send a turn to an assistant (for example from the dashboard). The server
   launches the matching `gram-asst-<assistant-id>` container automatically,
   publishes the runner port on an ephemeral loopback port, and reuses the
   container on later turns.

4. Iterate: rebuild the image with the same command. The next admission or
   recycle detects the image ID change and replaces idle containers with the
   new image — a container with a turn in flight is never interrupted, it
   rolls over on the next safe opportunity.

Each assistant gets a `gram-asst-work-<assistant-id>` Docker volume as its
workspace. It survives container stops and replacements, and is removed when
the assistant runtime is reaped.

### TLS

Runtime containers reach the server at `https://host.docker.internal:<port>`.
`mise run zero:tls` includes that hostname in the local certificate and writes
`GRAM_ASSISTANT_RUNTIME_LOCAL_CA_FILE` (the mkcert root CA) to
`mise.local.toml`; the server mounts that CA into runtime containers so the
runner trusts the local certificate. If you set up TLS before this hostname
was added, re-run `mise run zero:tls`.

### Inspecting and cleaning up

```sh
# containers, ports, images, volumes
mise run assistants:local-status
# stream runtime logs (also part of pitchfork daemons)
pitchfork logs --tail assistant-runtime
docker logs -f gram-asst-<assistant-id>
# remove all runtime containers + volumes
mise run assistants:local-clean
```

### Smoke test

1. `mise run build:assistants-runtime-image`
2. Start the stack and send a turn to any assistant.
3. `mise run assistants:local-status` — the assistant's container is `Up` with
   a `127.0.0.1:<port>->8081/tcp` publish, and the turn produces a reply.
4. `mise run assistants:local-clean` — containers and volumes are gone; the
   next turn relaunches from scratch.

### Migrating from the Fly.io workflow

If your `mise.local.toml` still pins `GRAM_ASSISTANT_RUNTIME_PROVIDER=flyio`
(or a `registry.fly.io/...` value for `GRAM_ASSISTANT_RUNTIME_OCI_IMAGE`),
remove those keys along with `GRAM_ASSISTANT_RUNTIME_FLYIO_*` to pick up the
local defaults.
