# Local Go workflows with Dagger

Run Go builds, lint, tests, and runtime image builds locally with
**Dagger 1.0.0-beta.21**, installed by mise. With Docker running, the Dagger CLI
starts its engine automatically. Application services and a Dagger Cloud account
are not required. GitHub Actions integration is deferred.

```bash
mise install http:dagger
mise run dagger:go build --component server export --path server/bin
mise run dagger:go lint --component server
mise run dagger:go-test --component server --shard 1/5 --race
mise run dagger:go-test --component server --packages ./internal/testenv
mise run dagger:go-test --component server --race --demo-seed
mise run dagger:go-test --component cli
mise exec -- dagger check --go-pipeline-project plog
```

`build` supports server (including tunnel-gateway), cli, plog, and hooks. Exported
binaries target Linux/amd64. `lint` supports server, cli, plog, and functions.
`dagger:go-test` also supports hooks, glint, and ci. Tests run on the engine's native
architecture. The `dagger check` project collection covers server, cli, plog, and
functions; its build check compiles packages without signing credentials.

The pipeline calls the existing `.mise-tasks/build`, `lint`, and `test` tasks.
Container tools are installed from `mise.toml` and `mise.lock`. Local configuration,
credentials, generated binaries, and test reports are excluded from source inputs.
Build tasks receive the workspace commit explicitly for version stamps.

## Tests and reports

Use `dagger:go-test` to export reports: it writes `junit-report.xml`, server
`test-timings.json`, `test-output.txt`, and `test-exit-code` into the component's
directory, then exits with the test status. It removes old reports before starting
and prints captured output on failure. Setup failures fail before report export.
The raw `dagger:go test ... export` operation returns reports even for failing tests;
its caller must check `test-exit-code`.

Custom `--packages` and `--race` are supported for server, cli, and functions.
The other components use their mise test tasks' fixed arguments.

Server tests start a disposable Docker daemon inside the test container for
testcontainers. This needs a privileged Dagger engine. The daemon shares the test
filesystem and network, including bind mounts and loopback-bound ports. The shard
containing the discovery SDK wire test installs its TypeScript dependencies.

## Images

```bash
mise run dagger:go server-image --binaries server/bin sync
mise run dagger:go tunnel-images

mise run dagger:go functions-packages \
  --signing-key file:/path/to/melange.rsa \
  --commit "$(git rev-parse --short HEAD)" \
  --build-date "$(git show -s --format=%cI HEAD)" \
  export --path functions/packages
mise run dagger:go functions-image \
  --packages functions/packages --public-key /path/to/melange.rsa.pub \
  --apko-config ./images/nodejs24-alpine3.23.yaml \
  --image functions-test:local --build-date "$(git show -s --format=%cI HEAD)" \
  export --path functions/oci/nodejs24
```

Functions packaging defaults to `x86_64`. Pass `--arch aarch64` to both operations
for a native ARM build. Cross-architecture nested Docker execution needs binfmt
support on the engine host. Both Node 22/Alpine 3.22 and Node 24/Alpine 3.23 configs
are supported, with the existing filesystem safety checks. Use a disposable
`melange keygen` keypair for local verification.

Publication is explicit: `publish-server` and `publish-functions` accept
docker/metadata-action's JSON and a Dagger secret (`--password env:VARIABLE`).
Functions publication filters tags by the exact `--image-prefix`, keeping dev and
production registry credentials separate; an empty prefix is rejected. Pass
`--arch aarch64` to `publish-functions` when publishing an ARM tarball. Obtain
registry credentials before calling these operations and pass them as a Dagger
secret.

## Local caching

The local engine caches tool-install layers, Go modules, compilation, lint, and
completed operations. Repeat an identical invocation to reuse its result. Changes
to the source snapshot invalidate source-dependent steps; Go cache volumes still
reuse unchanged packages. The nested Docker daemon is disposable.

## Updating Dagger

Beta archives are published at `dl.dagger.io` before GitHub Releases, so the CLI
uses mise's HTTP backend with published checksums. Update these together:

1. `mise.toml` version; run `mise install http:dagger` and `mise lock http:dagger`.
2. `.dagger/go/dagger-module.toml` engine version. The CLI starts the matching local
   engine.
3. Run `mise exec -- dagger generate -y` and commit the generated Go bindings and
   `dagger.lock`. After changing any exported pipeline signature, regenerate before
   calling the module. Upgrade the Go SDK with `dagger module update go-sdk` when
   required by the new engine.
4. Exercise tests, the server image smoke test, Functions packaging and both image
   configs, and `tunnel-images`. `mise exec -- go -C .dagger/go test ./...` runs the
   pipeline's report-wrapper tests; `mise exec -- go -C .dagger/go vet ./...` checks
   the implementation itself.

The image import regression tests use a live Dagger session:

```bash
mise exec -- dagger api with-session -- go -C .dagger/go test -tags=dagger_integration ./...
```
