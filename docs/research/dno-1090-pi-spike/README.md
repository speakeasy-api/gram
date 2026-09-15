# DNO-1090 spike: MCP distribution into Pi

Throwaway spike code for `docs/research/dno-1090-mcp-distribution-into-pi.md`.
Not a shipping implementation, not wired into any build, and not covered by CI.
It exists so the claims in that document can be re-run.

## What is here

| Path                             | Purpose                                                                                                                                                                                                                                                                                                            |
| -------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `extensions/gram-bridge.ts`      | The interesting part: a Gram-owned MCP client living inside a Pi extension. Reads a Gram-generated `mcp.json`, speaks MCP streamable HTTP over `fetch` with no npm dependency, republishes each discovered tool through `pi.registerTool()`, and carries the observability and policy handlers in the same module. |
| `extensions/gram-bridge-sdk.ts`  | Same bridge on `@modelcontextprotocol/sdk`, to compare against the dependency-free variant.                                                                                                                                                                                                                        |
| `gram-claude-dialect.mcp.json`   | Real generator output (`generateMCPFiles` in `server/internal/plugins`), with the MCP URL pointed at the local stand-in. Not hand-written.                                                                                                                                                                         |
| `gram-opencode-dialect.mcp.json` | Same, in Gram's OpenCode dialect (`mcp` key, `type: remote`), to show one bridge reads both.                                                                                                                                                                                                                       |
| `harness/gram-mcp-server.mjs`    | Stand-in for a Gram-hosted MCP server: streamable HTTP on one URL, bearer auth in an `Authorization` header, Gram-style tool names. Records every `tools/call` on `/calls`.                                                                                                                                        |
| `harness/scripted-model.mjs`     | OpenAI-compatible model that returns a scripted tool call, so Pi runs headless with no real LLM. Rejects an unknown tool name and echoes the tool list it was offered, which is how `probe-tool-names.sh` reads Pi's tool list.                                                                                    |
| `harness/scripted-provider.ts`   | Registers that model with Pi via `pi.registerProvider()`.                                                                                                                                                                                                                                                          |
| `run.sh`                         | Nine assertions across config dialects, credential handling, policy denial, both bridge variants, and package installation.                                                                                                                                                                                        |
| `probe-tool-names.sh`            | Prints the tool list Pi offers the model under each distribution option.                                                                                                                                                                                                                                           |

The MCP server is a stand-in rather than a live Gram tenant: it reproduces the
contract Gram's generated `mcp.json` points at, which is the part Pi has to
talk to. The config it is driven by is genuine generator output.

## Running

```bash
pnpm install --ignore-workspace
./run.sh
./probe-tool-names.sh
```

Needs Node and outbound npm access on first install. `probe-tool-names.sh`
additionally installs `npm:pi-mcp-adapter` into a temporary Pi home to compare
against the community option; nothing is written to your real `~/.pi`.

Ports 8931, 8932, 8933 and 8936 must be free.

## Regenerating the Gram configs

The two `*.mcp.json` files came from a temporary test in
`server/internal/plugins` that called `generateMCPFiles` with one private
server whose `MCPURL` was `http://127.0.0.1:8931/mcp`, and wrote the resulting
file map to disk. That test was deleted; recreate it if the generator's output
shape changes.
