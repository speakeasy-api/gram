# OpenClaw observability distribution through ClawHub

Research for DNO-1424, checked 28 October 2026. Recommendation: publish a
credential-free, versioned plugin and provision each installation separately.
Do not publish the existing generated deployment archive to a public registry.
This document proposes the next increment; no package has been published or
live installation qualified.

## What a listing requires

ClawHub supports code plugins, not only skills. A publisher needs an account
with permission for the chosen owner and package scope. Code packages need an
`openclaw.plugin.json` manifest and package compatibility/build metadata,
including `openclaw.compat.pluginApi` and `openclaw.build.openclawVersion`.
Record the source repository and exact source revision. Validate the package,
inspect the dry-run plan, and then publish; publication remains pending until
registry checks pass. A successful upload alone is not installation evidence.
See [ClawHub publishing](https://github.com/openclaw/clawhub/blob/main/docs/publishing.md).

Use built JavaScript entry points and an explicitly tested runtime range, as
described in [Building plugins](https://docs.openclaw.ai/plugins/building-plugins).
The generated Speakeasy package already has `index.js`, a manifest, and startup
activation, but currently sets version `0.0.0`, `private: true`, and no
compatibility/build metadata. Its runtime files include deployment-specific
`speakeasy.json`. See
[`generateOpenClawObservabilityPluginInDir`](../../server/internal/plugins/generate.go)
and `writeHooksRuntimeFiles` in the same file. These are a starting point for a
release package, not a publishable registry artifact.

Use an explicitly qualified, pinned `clawhub:<package>@<version>` installation
in qualification. Current documentation distinguishes released versions from
development builds that infer ClawHub from an unqualified name. A registry
source can remove the arbitrary-local-source trust prompt; it does not justify
silently accepting conversation access or every capability. Verify the actual
flags and permission prompts on each supported release. Sources:
[ClawHub overview](https://docs.openclaw.ai/clawhub) and
[Plugins](https://docs.openclaw.ai/plugins).

## Provision credentials outside chat

Separate package distribution from authorization. The public artifact must be
identical across organizations and contain no API key, organization identifier,
install token, or configured private endpoint.

The relay already accepts `SPEAKEASY_AI_HOOKS_API_KEY` before cached login or
the configured key; see [`resolveAuth`](../../hooks/relay/auth.go). An operator
can supply that environment variable to the Gateway service through its secret
manager, outside a model conversation. This keeps the key out of the package
and chat, but is not proof that an agent with local process/file access cannot
read it. Verify child-process inheritance and host access boundaries.

OpenClaw SecretRefs support selected credential surfaces. They are not a
drop-in replacement for arbitrary strings in Speakeasy's own config file.
A future adapter must explicitly resolve a supported secret surface or use a
broker, and test how the relay obtains it. The official
[secrets documentation](https://docs.openclaw.ai/gateway/secrets) warns that
plaintext configuration and environment files remain readable to agents that
can inspect them. Do not claim SecretRef support for this plugin yet.

For guided setup, retain the existing one-use, fifteen-minute install-code
mechanism in [`agent_install.go`](../../server/internal/mcp/agent_install.go),
but adapt the proposed OpenClaw installer to configure an installed registry
package instead of embedding credentials in its distributable files. Treat
the install code as a bearer secret too: redeem it in a human-operated terminal
or trusted installer, never send it through chat, and avoid command/output
logging. Long-lived keys stay on the target machine's protected secret path.

## Comparison and acceptance

| Approach                  | Distribution             | Per-install identity                 | Main tradeoff                                    |
| ------------------------- | ------------------------ | ------------------------------------ | ------------------------------------------------ |
| Current generated archive | Local ZIP                | Embedded deployment config           | Simple setup, but artifact must remain private   |
| Proposed ClawHub package  | Public versioned package | Separately provisioned secret/config | Reusable updates; needs a new configuration path |

The agent-install work in PRs #8088/#8108 remains useful for enrollment and
qualification; a listing does not replace those flows. Before implementing a
release pipeline, qualify a credential-free package against pinned OpenClaw
versions: validate and dry-run it, install it, configure a synthetic identity,
run one real prompt/tool turn, check agent attribution and exactly one prompt,
restart, update, revoke the key, and uninstall. Inspect artifacts and logs for
secret residue. Record capability prompts and missing-credential behavior.

Unresolved: publisher ownership, minimum supported release, the config path
outside the updateable plugin directory, and a supported secret-resolution
contract. No registry release or production credential migration is implied
by this research PR.
