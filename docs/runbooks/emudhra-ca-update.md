# eMudhra CA mitigation — 2026-10-20

The [incident advisory](https://emudhra.we-have-a-problem.com/) reports compromised
intermediate CA keys. Most non-browser TLS clients do not consume the browser
revocation lists. Updating a runtime or an image tag alone does not prove that
its TLS clients reject these chains.

## Production images

| Image/source                                  | Change                                                                                                                                                        | Coverage                                                                                                                                                                                                                 |
| --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `server/Dockerfile`                           | Refresh pinned `static-debian13:nonroot` digest to `516757106de3…`.                                                                                           | Verified bundle excludes the affected roots. Server and tunnel-gateway binaries are built separately with Go 1.27.3.                                                                                                     |
| `pystreams/Dockerfile.pystreams`              | Move runtime from Debian 12 to pinned `cc-debian13:nonroot` (`b7e81cb1af38…`); Python 3.14.8 and uv 0.13.4 in the builder.                                    | Debian 12's latest bundle still contains both affected Linux roots; Debian 13 removes them. Covers system-store TLS in addition to the workspace's updated certifi.                                                      |
| `agents/runtime-image/Dockerfile`             | Share a pinned Debian bookworm base (`7c7b2c966bc9…`); filter and fingerprint-check the system store; generate explicit OpenSSL rejection entries for Bun.    | Covers the runtime, dependency install, and Lightpanda download stages. Bun loads the rejection bundle through `SSL_CERT_FILE`; adding a local CA preserves it. The runner also needs the Rust dependency rebuild below. |
| `otel-collector/Dockerfile`                   | Update upstream collector 0.157.0 → 0.163.1 (`1175b0300305…`); copy the verified Debian 13 bundle into the final image.                                       | Upstream 0.163.1 still uses Go 1.26.9, before Go's fix. Its `/etc/ssl/certs` contains only the bundle, so replacing it removes the affected default trust paths.                                                         |
| `tunnel/Dockerfile`                           | Go 1.27.3 builder; install full Alpine CA package and apply the distrust workaround at build time.                                                            | Covers the published tunnel agent image and its system trust store.                                                                                                                                                      |
| `functions/images/nodejs{22,24}-*.yaml`       | Install full `ca-certificates`; filter the roots and regenerate the store in the root entrypoint **before** initialization/network access and privilege drop. | `functions/internal/bootstrap/prepare.go` passes `--use-openssl-ca` explicitly. The function subprocess has a fresh environment, so image-level `NODE_OPTIONS` alone would not work.                                     |
| `functions/images/python3.12-alpine3.22.yaml` | Python 3.12.15 and the same entrypoint CA filtering.                                                                                                          | Updates the available Python image definition; the current publishing matrix only includes Node.js 22 and 24. User-provided Python packages with their own CA bundles need their own updates.                            |

Full multi-platform image digests are recorded in the Dockerfiles. No image
build or deployment to production is performed by this change itself.

The Debian/Alpine workaround follows the advisory: disable
`mozilla/emSign_ECC_Root_CA_-_G3.crt` and, where present,
`mozilla/Comodo_AAA_Services_root.crt`. `update-ca-certificates` updates both the
bundle and certificate-directory links. This distrusts **all** certificates
whose only valid path ends at these roots, including otherwise legitimate ones.

### Scope of the production audit

`.github/workflows/pr.yaml` builds/publishes server, Python streams, assistant
runtime, collector, Functions, and dashboard/admin images. The collector is also
deployed to Fly from that workflow. `tunnel/Dockerfile` is published by the PR and
release workflows.

Dashboard/admin nginx images serve static files; their checked-in nginx configs
do not make outbound TLS requests. Local Compose infrastructure, the CI-only
ClickHouse test image, and local database tooling are not production application
images and are outside this update.

## Toolchain and dependency changes

- Go 1.27.1 → **1.27.3** in mise and all three Go modules; regenerate mise locks.
- Node.js 24.16.0 → **24.22.0 LTS** in mise and `.node-version`.
- Pin workspace Python to **3.14.8** in `.python-version`. Python 3.15.0 was
  attempted, but locked native dependencies lack 3.15 wheels (including cffi
  and DuckDB), and the existing slim build has no compiler. Python's interpreter
  update is independent of the CA fix.
- uv → **0.13.4** in mise and the Python image builder.
  Both the Python builder and uv images are pinned to multi-platform digests.
- golangci-lint → **2.14.0**, including the server custom-build pin. Version
  2.13.2 cannot decode this Go toolchain's export data. 2.14.0 supports it;
  2.15.0 introduces unrelated repository-wide lint-rule migrations.
  Exempt protobuf wire field numbers from enum exhaustiveness checking: these
  are schema-defined integers, so maps need not contain protobuf's range constants.
- certifi → **2026.10.19** in `uv.lock`, with a workspace minimum constraint.
- Assistant `Cargo.lock`: rustls-webpki **0.103.16**, webpki-roots and
  webpki-root-certs **1.0.10**, plus the resolver-required aws-lc updates.
- SDK generation runs its Go build, test compilation, and lint explicitly with
  Staticcheck **0.8.2**, because the pinned generator's built-in 0.8.1 cannot read
  Go 1.27 export data. Lint failures still fail generation.

## Upstream follow-up and trust-store boundaries

- **Bun:** `SSL_CERT_FILE` normally adds to its bundled roots. The assistant
  image generates a separate bundle containing OpenSSL `TRUSTED CERTIFICATE`
  entries with explicit `serverAuth` and `clientAuth` rejection for affected
  embedded roots. Bun loads these entries before its bundled roots, so the
  duplicate embedded certificates cannot restore trust. Build-time tests check
  this precedence across its TLS APIs. Preserve the auxiliary rejection data:
  converting these entries to ordinary PEM certificates would discard it.
- **Node.js:** upstream's [CA fix](https://github.com/nodejs/node/pull/66871) is
  pending. Hosted Functions use the filtered system store via an explicit flag;
  ordinary local/CI Node 24.22.0 still uses its bundled roots by default.
- **Lightpanda:** version 0.2.8's
  [`Network.loadCerts`](https://github.com/lightpanda-io/browser/blob/0.2.8/src/network/Network.zig)
  scans the system certificate store and passes it to libcurl. The updated
  assistant system store covers this path; no unrelated browser version jump
  is needed.
- Prebuilt developer tools and Docker itself have independent Go/Rust builds
  and trust stores. Updating this repository's compiler does not rebuild them.
  Check vendor advisories and each tool's effective trust store separately.

## Verification

Extract the bundle from each built image without running its application:

```sh
(
  set -eu
  image='<IMAGE_TAG_OR_DIGEST>'
  audit_dir=$(mktemp -d)
  cid=$(docker create "$image" /unused)
  trap 'docker rm -f "$cid" >/dev/null; rm -rf "$audit_dir"' EXIT
  docker cp "$cid:/etc/ssl/certs/ca-certificates.crt" "$audit_dir/ca-certificates.crt"
  mise exec -- uv run --no-sync python docs/runbooks/check-emudhra-ca.py "$audit_dir/ca-certificates.crt"
)
```

For Functions, the raw apko layer contains the distribution's unfiltered bundle.
Run its CA regeneration commands in an isolated running container before copying
the bundle. This exercises the trust-store setup without requiring a function
archive or API credentials:

```sh
(
  set -eu
  image='<FUNCTIONS_IMAGE_TAG_OR_DIGEST>'
  audit_dir=$(mktemp -d)
  cid=$(docker run -d --entrypoint sh "$image" -c 'sleep 300')
  trap 'docker rm -f "$cid" >/dev/null; rm -rf "$audit_dir"' EXIT
  docker exec "$cid" sh -ec '
    sed -i -e "s|^mozilla/emSign_ECC_Root_CA_-_G3.crt|!&|" \
      -e "s|^mozilla/Comodo_AAA_Services_root.crt|!&|" /etc/ca-certificates.conf
    update-ca-certificates
  '
  docker cp "$cid:/etc/ssl/certs/ca-certificates.crt" "$audit_dir/ca-certificates.crt"
  mise exec -- uv run --no-sync python docs/runbooks/check-emudhra-ca.py "$audit_dir/ca-certificates.crt"
)
```

The checker uses the four DER SHA-256 fingerprints
from the advisory's [roots.json](https://emudhra.we-have-a-problem.com/roots.json),
and fails on an empty bundle. It checks contents, not which store a client uses.
Also check that normal HTTPS works and affected chains fail in the actual
runtime; a clean OS bundle is insufficient evidence for a bundling client.

### Results on the local Linux arm64 builder

- Built server, Python streams, assistant runtime, collector, and tunnel images.
- All five final CA bundles pass the fingerprint check. The unchanged Debian 12
  distroless bundle fails, providing a negative control for the checker.
- The pinned Debian 13 static and cc **amd64** bundles also pass the same check.
- Python image imports spaCy, NumPy, and grpcio on Debian 13. Both its standard
  library TLS client and certifi accept `https://example.com` and reject both
  advisory probes (`dv-g3b-emudhra.we-have-a-problem.com` and
  `dv-g3b-aaa-emudhra.we-have-a-problem.com`) with certificate-verification errors.
- Assistant curl and Lightpanda accept the control site and reject both probes.
  Lightpanda reports `PeerFailedVerification` in its error page; its process exit
  code alone does not distinguish navigation success from failure.
- Bun now accepts the control site and rejects both probes, including after
  `/init` combines a mounted extra CA with its rejection bundle. A probe using
  the runner's locked reqwest dependencies also succeeds with this bundle.
- Seven offline tests run during every assistant base build: filename-independent
  fingerprint checking, empty-bundle rejection, and explicit distrust across
  Bun fetch, HTTPS, TLS, WebSocket, and `Bun.connect`'s handshake authorization
  result. Tests use a locally generated CA to exercise both accepted and rejected
  connections without relying on external probe availability.
- Built both production apko Functions images and exercised their actual
  entrypoints with a local fixture archive. Node 22 and 24, running without an
  inherited environment and with `--use-openssl-ca`, accept the control site and
  reject both probes. Alpine currently supplies Node 22.23.2 and 24.18.1 for these
  images; the mise LTS pin does not select Alpine package versions.
- Both apko image safety suites pass. The built Functions runner, server, and
  tunnel-gateway binaries report Go 1.27.3.
- Collector 0.163.1 validates the checked-in configuration with a placeholder
  API key. Its copied bundle is clean, while its binary still reports Go 1.26.9.
- 82 Functions Go tests pass, including a real TLS test of both Node launch
  paths with an isolated environment and a custom OpenSSL trust file.
- Server and Functions lint pass with golangci-lint 2.14.0; the CA checker passes Ruff.
- SDK source checks and full regeneration pass with the explicit Go validation
  pipeline. The SDK check regression test confirms that Staticcheck failure
  still fails generation.
- 76 assistant Rust tests pass inside the image's compiler stage (the host lacks
  a C compiler). Earlier validation also passed dashboard type-checking and
  Python tests; one timing-sensitive infra test passed on retry.

The multi-platform pins select matching upstream variants. Actual runtime
execution checks above were performed on arm64; amd64 verification checked the
distroless trust bundles without executing the application.

## Rollout and follow-up

1. Build/publish the updated images and binaries through their normal pipelines.
   Ensure the new assistant `agents/` content hash is selected; a base-image
   refresh does not update an already-published content-addressed image.
2. Roll server/worker/stream workloads, Python streams, and collector to the new
   image digests. Replace existing assistant containers/warm-pool instances.
3. Publish both Functions runtime images and select their new tags in the
   deployment configuration. Existing Fly function machines retain their old
   images until replaced/redeployed. Publish the tunnel agent release too.
4. Run the bundle/runtime checks on the **published platform variants** and
   confirm the running workload digests. Merge/build success alone is not a
   completed production rollout.
5. Remove temporary root filtering only when the selected distribution bundles
   exclude the affected roots, or every relevant verifier blocks the compromised
   intermediates. Remove the collector bundle override only after verifying its
   replacement image. Recheck Bun and Node upstream releases separately.
