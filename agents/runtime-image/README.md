# Speakeasy Assistant Runtime Image

The container image assistant runtimes run in: the Rust runner
(`agents/runner`), the bun-based sandbox, and a lightpanda browser.

## Local development

Local development uses the `local` assistant runtime provider. `./zero`
(including `./zero --agent`) runs `mise run zero:assistants`, which writes
`GRAM_ASSISTANT_RUNTIME_PROVIDER=local` and
`GRAM_ASSISTANT_RUNTIME_OCI_IMAGE=gram-assistant-runtime` into `mise.local.toml`
when those vars are unset, and builds `gram-assistant-runtime:dev` if the image
is missing. The Speakeasy server then starts one runtime container per assistant on
your machine's Docker daemon, on demand. No remote runtime credentials or
registry pushes are involved.

`mise run zero:assistants --restart` rewrites those keys and rebuilds the image.

### Workflow

1. First-time setup is `./zero` (or `mise run zero:assistants`). To rebuild
   the image later:

   ```sh
   mise run build:assistants-runtime-image
   ```

2. Start Speakeasy normally (e.g. `./zero --agent`).

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

1. `mise run zero:assistants` (or `mise run build:assistants-runtime-image`)
2. Start the stack and send a turn to any assistant.
3. `mise run assistants:local-status` — the assistant's container is `Up` with
   a `127.0.0.1:<port>->8081/tcp` publish, and the turn produces a reply.
4. `mise run assistants:local-clean` — containers and volumes are gone; the
   next turn relaunches from scratch.

## Native Agent Identity assertion refresh

`kit-workload-token.mjs` refreshes an **unbound** Agent Identity ID token from
GKE metadata. The Pod must have its native Agent Identity annotation and use
an unlinked Kubernetes service account; this helper does not impersonate a
Google service account. No Python dependency or certificate mount is required.

Required environment (all supplied independently by the backend):

- `WORKLOAD_TOKEN_AUDIENCE`: exact exchange audience.
- `WORKLOAD_TOKEN_ISSUER`: exact admitted Agent Identity issuer.
- `WORKLOAD_TOKEN_SUBJECT`: exact admitted `spiffe://` subject.
- `WORKLOAD_TOKEN_PATH`: absolute assertion path on the private memory-backed
  volume, not the persisted workspace or HOME.

The wire request is:

```http
GET /computeMetadata/v1/instance/service-accounts/default/identity?audience=<URL-encoded-audience>&format=full HTTP/1.1
Host: 169.254.169.254
Metadata-Flavor: Google
```

There is no request body, certificate-chain binding, or `bind_id_token` query
parameter. This matches `fetch_id_token(request, audience, bind_id_token=False)`
in the published [google-auth 2.62.0 wheel](https://pypi.org/project/google-auth/2.62.0/):
`google/oauth2/id_token.py` forwards the option to `IDTokenCredentials`;
`google/auth/compute_engine/credentials.py::_call_metadata_identity_endpoint`
sets the path, audience and `format=full`; and
`google/auth/compute_engine/_metadata.py::_build_token_request_options`
returns `GET`, no body, without reading a certificate when binding is false.
`_metadata.get` adds `Metadata-Flavor: Google`. The Python library also adds
an `x-goog-api-client` telemetry header, which this helper intentionally omits.
The link-local address reaches the same metadata service without DNS.

The helper requires the response metadata header, rejects redirects, limits
response bodies to 16 KiB, and aborts requests (including body reads) after ten
seconds. Local checks require RS256, exact issuer/subject and audience (a matching string or single-element array),
integer times, no future issue time beyond 30 seconds, positive lifetime no
longer than one hour, and more than 60 seconds remaining. Any `cnf` claim,
even null, is rejected. These are shape checks, not signature verification;
the exchange server verifies the signature and admitted identity.

Publication uses a mode-0600 temporary file and atomic rename. `--ready` gates
startup on a usable assertion. Refresh retries retain only a still-usable old
assertion and remove it once it is unusable. Metadata tokens and upstream
errors are never logged. The memory-backed volume and single-writer ownership
remain the Pod's responsibility.

The existing runtime-image PR pipeline must build and publish this helper,
then the deployment must select the resulting image digest before native
Agent Identity refresh is enabled. Reusing an older runtime digest does not
pick up this change.
