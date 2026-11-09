# Local MCP fixture

The synthetic upstream is explicitly opt-in. It runs on IPv4 loopback under
Pitchfork and is excluded from normal development startup groups.

Prepare the stack's development certificate first (`./zero` normally does this).
The certificate must cover `127.0.0.1`. The tasks use the existing
`GRAM_SSL_CERT_FILE`, `GRAM_SSL_KEY_FILE` and local mkcert CA; they never disable
certificate verification or install a new trust root.

```sh
GRAM_MCP_FIXTURE=local mise run fixture:start --dry-run
GRAM_MCP_FIXTURE=local mise run fixture:start
mise run fixture:smoke --dry-run
mise run fixture:smoke
mise run fixture:stop --dry-run
mise run fixture:stop
```

`fixture:env` prepares bindings without starting the daemon. It writes the
ignored `.local/mcp-fixture.env`, with mode 0600, and preserves the generated
control secret across restarts. Tasks read this dotenv file internally; the secret
is never printed. Supply its `FIXTURE_BASE_URL` and `FIXTURE_CONTROL_SECRET` as
private runner inputs. Restart loses runtime scenarios and invalidates their boot
identity; flows must create fresh scenarios and reject a changed boot ID.

`GRAM_MCP_FIXTURE_PORT` and `GRAM_MCP_FIXTURE_CLIENT_PORT` join the normal worktree
port remapping. Their primary-checkout defaults are 18750 and 18751. A linked
worktree's first preparation allocates missing fixture ports through the existing
remapper and reserves only those two keys in ignored `mise.local.toml`; it does
not change application bindings or start the stack. Preparation verifies both
ports are available before creating or changing bindings. Explicit `--port` and
`--client-port` overrides are available; stop the fixture before changing its
settings. The second port belongs only to the short-lived synthetic HTTPS client
used by the smoke test.

The OAuth origin ceiling contains the configured worktree server and dashboard
origins plus that isolated smoke client. The fixture receives `SSL_CERT_FILE`
pointing at the local CA, so its CIMD fetches can verify the stack's HTTPS
certificate. The smoke checks both directions of TLS using an owned synthetic
client; it requires no application stack, account login or browser. It validates
DCR and CIMD, PKCE, HTTPS callbacks, refresh, tool echo, control authentication,
scenario cleanup and upstream events. Gateway behavior is tested separately by
the Reflow flows against the running stack.

The fixture executable uses explicit `mise exec` with immutable revision
`7cec927eaab8348c0699c6faf547843e3bf9f4ae`. The private benchmark package is not
in the global tool set, so normal `mise install`, setup and CI do not fetch it.
Only an actual fixture launch resolves and installs it. That launch requires
Git authentication with read access to `speakeasy-api/benchmark-mcp-server`;
mise's GitHub token alone does not provide Go's Git authentication. It sets
`GOPRIVATE`, `GONOPROXY` and `GONOSUMDB` for that module in the child process,
preserving existing patterns without changing user configuration.

To test an unpushed build, pass an absolute
`--bin /path/to/benchmark-mcp-server` to `fixture:start` or `fixture:env`. This
override stays in the private local env file and bypasses private installation.
Dry-run does not resolve or install the private tool. Use `--cert-file`,
`--key-file` and `--ca-file` for an existing development certificate in another
owned checkout. A nested clone with the same directory name as its parent needs
a unique `namespace` in its ignored `pitchfork.local.toml` before starting.

Native TLS and the outbound tunnel cannot share a listener yet. For a local
tunnel fixture, run a separate HTTP process or use TLS termination at ingress.
