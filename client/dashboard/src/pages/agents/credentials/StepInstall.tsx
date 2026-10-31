import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { useState, type JSX } from "react";
import { CodeBlock } from "./CodeBlock";
import { WizardStepHeader } from "./WizardChrome";
import { isCredentialSafe } from "./gateway";

/**
 * The step that hands the runtime its credential. One command for a machine
 * with a shell, the values themselves for a runtime configured by hand, and
 * the same pair in code for an SDK — each is the same key, shown once.
 */

const KEY_ENV = "SPEAKEASY_AGENT_KEY";

export function StepInstall({
  command,
  commandError,
  minting,
  onRegenerate,
  onIssue,
  issuing,
  secret,
  gatewayURL,
  serverCount,
}: {
  command: string | null;
  commandError: string | null;
  minting: boolean;
  onRegenerate: () => void;
  /** Issue a key, for a step reached without one to show. */
  onIssue: () => void;
  issuing: boolean;
  secret: string | null;
  gatewayURL: string;
  /** `"all"` where the agent's reach is a wildcard rather than a list. */
  serverCount: number | "all";
}): JSX.Element {
  const [tab, setTab] = useState("one-line");
  // Every recipe here puts a bearer key on the wire, and the key itself is on
  // screen beside them, so a plaintext endpoint would hand it to anyone on
  // the path. Loopback is exempt: it never leaves the machine, and local
  // development runs there. Empty means no agent exists yet, which has no
  // endpoint to judge — the panel below is covered in that case anyway.
  const unsafeTransport = gatewayURL !== "" && !isCredentialSafe(gatewayURL);
  // Counting is not available for a wildcard, and a count taken from today's
  // inventory would be a number that quietly changes behind the endpoint.
  const reachPhrase =
    serverCount === "all"
      ? "every server"
      : `${serverCount} ${serverCount === 1 ? "server" : "servers"}`;
  const reachClause =
    serverCount === "all"
      ? "Every server sits"
      : `${serverCount} ${serverCount === 1 ? "server sits" : "servers sit"}`;
  const key = secret ?? `<your ${KEY_ENV}>`;
  // One complete call per runtime, not the pair of values on their own: the
  // question the step has to answer is what to do with them.
  // Streamable HTTP, not the SDK's built-in "sse" transport: the gateway
  // answers POST only, and an SSE client opens with a GET it would refuse.
  const vercel = [
    `import { experimental_createMCPClient as createMCPClient } from "ai";`,
    `import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";`,
    ``,
    `const client = await createMCPClient({`,
    `  transport: new StreamableHTTPClientTransport(new URL("${gatewayURL}"), {`,
    `    requestInit: {`,
    `      headers: { Authorization: \`Bearer \${process.env.${KEY_ENV}}\` },`,
    `    },`,
    `  }),`,
    `});`,
    ``,
    `const tools = await client.tools();`,
  ].join("\n");

  const mastra = [
    `import { MCPClient } from "@mastra/mcp";`,
    ``,
    `const mcp = new MCPClient({`,
    `  servers: {`,
    `    gram: {`,
    `      url: new URL("${gatewayURL}"),`,
    `      requestInit: {`,
    `        headers: { Authorization: \`Bearer \${process.env.${KEY_ENV}}\` },`,
    `      },`,
    `    },`,
    `  },`,
    `});`,
    ``,
    `const tools = await mcp.getTools();`,
  ].join("\n");

  // Connected and handed to an agent, not just constructed: the server is
  // only a description of the endpoint until a session is open on it, so a
  // block that stops at the constructor copies into code that does nothing.
  // The session is async, so it sits inside a coroutine `asyncio.run` drives:
  // `async with` and `await` at module scope are a syntax error.
  const python = [
    `import asyncio`,
    `import os`,
    `from agents import Agent, Runner`,
    `from agents.mcp import MCPServerStreamableHttp`,
    ``,
    `async def main():`,
    `    async with MCPServerStreamableHttp(`,
    `        params={`,
    `            "url": "${gatewayURL}",`,
    `            "headers": {`,
    `                "Authorization": f"Bearer {os.environ['${KEY_ENV}']}"`,
    `            },`,
    `        },`,
    `    ) as server:`,
    `        agent = Agent(name="Assistant", mcp_servers=[server])`,
    `        result = await Runner.run(agent, "What tools do you have?")`,
    `        print(result.final_output)`,
    ``,
    `asyncio.run(main())`,
  ].join("\n");

  if (unsafeTransport) {
    return (
      <div className="space-y-5">
        <WizardStepHeader
          title="Connect the agent"
          description="This deployment cannot carry an agent key."
        />
        <Text role="alert">
          MCP is served over plaintext HTTP at {gatewayURL}. Connection
          instructions are withheld because they would send the agent key
          unencrypted. Configure an HTTPS server URL and reload.
        </Text>
      </div>
    );
  }

  return (
    <div className="space-y-5">
      <WizardStepHeader
        title="Connect the agent"
        description="Pick how its runtime will be configured."
      />
      {/* Every tab here hands over the same key, in a different shape, so
          none of them has anything to show until one is issued. Covering the
          whole box states that once rather than three times, and stops a
          reader copying a template with a placeholder where the key goes. */}
      <div className="relative">
        <div
          className={cn(
            "border-border border",
            !secret && "pointer-events-none select-none blur-[2px]",
          )}
          aria-hidden={!secret}
          inert={!secret ? true : undefined}
        >
          <Tabs value={tab} onValueChange={setTab} className="gap-0">
            <div className="border-border bg-muted/30 border-b px-4">
              <PageTabsList>
                <PageTabsTrigger value="one-line">
                  One-line setup
                </PageTabsTrigger>
                <PageTabsTrigger value="manual">Manual</PageTabsTrigger>
                <PageTabsTrigger value="programmatic">
                  Programmatic
                </PageTabsTrigger>
              </PageTabsList>
            </div>

            <TabsContent
              value="one-line"
              forceMount
              className="data-[state=inactive]:hidden"
            >
              <div className="space-y-4 p-4">
                <Text muted small>
                  For runtimes with a shell: OpenClaw, Claude Code, Codex,
                  Cursor and CI runners. Run once on the machine the agent lives
                  on.
                </Text>
                {command ? (
                  <CodeBlock
                    value={command}
                    label="setup command"
                    language="shell"
                  />
                ) : (
                  <Text muted small>
                    {minting ? "Preparing the setup command…" : ""}
                  </Text>
                )}
                <div className="space-y-2">
                  <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
                    The script
                  </span>
                  <ol className="space-y-1 text-sm">
                    {[
                      "Exchanges the setup code for this agent's API key.",
                      `Configures the MCP clients it finds on PATH with the agent's gateway (${reachPhrase}).`,
                      "Leaves the key out of your shell history: the code expires after 15 minutes.",
                      "Reports what it configured, and exits non-zero if it found no client.",
                    ].map((line, index) => (
                      <li key={line} className="flex gap-3">
                        <span className="text-muted-foreground font-mono text-xs">
                          {String(index + 1).padStart(2, "0")}
                        </span>
                        <span>{line}</span>
                      </li>
                    ))}
                  </ol>
                </div>
                {/* Only alongside a link there is one of: regenerating
                  without a key to wrap is an action with nothing behind it,
                  and an underlined word that silently does nothing reads as
                  broken rather than as unavailable. */}
                {secret && (
                  <Text muted small>
                    The setup link expires in 15 minutes.{" "}
                    <button
                      type="button"
                      className="underline disabled:no-underline disabled:opacity-60"
                      disabled={minting}
                      onClick={onRegenerate}
                    >
                      Regenerate
                    </button>
                  </Text>
                )}
                {command && (
                  <Text muted small>
                    Piping to a shell runs whatever this deployment returns. To
                    read it first, fetch it to a file and run that instead. The
                    code stays valid for 15 minutes, so you can rerun it. The
                    saved file carries the key: delete it once you have run it.
                  </Text>
                )}
                {commandError && (
                  <Text role="alert" small>
                    {commandError}
                  </Text>
                )}
              </div>
            </TabsContent>

            <TabsContent
              value="manual"
              forceMount
              className="data-[state=inactive]:hidden"
            >
              <div className="space-y-4 p-4">
                <Text muted small>
                  For a runtime you configure by hand. Each block is the whole
                  configuration for that client — paste it, with the key in the
                  environment as <code className="text-xs">{KEY_ENV}</code>.
                </Text>
                <div className="space-y-2">
                  <CodeBlock
                    value={key}
                    label="API key — shown once"
                    language="shell"
                    breakAnywhere
                  />
                  <Text muted small>
                    Put it where that runtime reads its secrets. The blocks
                    below name it rather than carry it, so they are safe to
                    paste into a repository.
                  </Text>
                </div>
                <CodeBlock
                  value={[
                    // No "# Claude Code" comment: the block is labelled that.
                    `claude mcp add --transport http gram ${gatewayURL} \\`,
                    `  --header "Authorization: Bearer $${KEY_ENV}"`,
                  ].join("\n")}
                  label="Claude Code"
                  language="shell"
                />
                <CodeBlock
                  value={[
                    "# ~/.codex/config.toml",
                    "[mcp_servers.gram]",
                    `url = "${gatewayURL}"`,
                    `bearer_token_env_var = "${KEY_ENV}"`,
                  ].join("\n")}
                  label="Codex"
                  language="toml"
                />
                <CodeBlock
                  value={[
                    // Single quotes keep the key out of argv and out of the
                    // config: OpenClaw saves the `${VAR}` reference and fills it
                    // in from its own environment when it connects. `add`'s
                    // pre-save check does not, hence --no-probe. The entry is
                    // named `speakeasy` like the one-line setup's.
                    `openclaw mcp add speakeasy --url ${gatewayURL} \\`,
                    "  --transport streamable-http --no-probe \\",
                    `  --header 'Authorization=Bearer \${${KEY_ENV}}'`,
                  ].join("\n")}
                  // The oldest release verified to take --no-probe and
                  // resolve the ${VAR} reference at connect time.
                  label="OpenClaw 2026.10.6 or later"
                  language="shell"
                />
                <CodeBlock
                  value={JSON.stringify(
                    {
                      mcpServers: {
                        gram: {
                          type: "http",
                          url: gatewayURL,
                          headers: {
                            Authorization: `Bearer \${env:${KEY_ENV}}`,
                          },
                        },
                      },
                    },
                    null,
                    2,
                  )}
                  label="mcp.json — Cursor, Windsurf, and others"
                  language="json"
                />
                <Text muted small>
                  {reachClause} behind that one endpoint. Changing what this
                  agent may reach changes what it finds there, without
                  reconnecting.
                </Text>
              </div>
            </TabsContent>

            <TabsContent
              value="programmatic"
              forceMount
              className="data-[state=inactive]:hidden"
            >
              <div className="space-y-4 p-4">
                <Text muted small>
                  The endpoint speaks MCP over HTTP, so any SDK that takes an
                  MCP server takes this one. Read the key from the environment;
                  never commit it.
                </Text>
                <CodeBlock
                  value={vercel}
                  label="Vercel AI SDK"
                  language="typescript"
                />
                <CodeBlock
                  value={mastra}
                  label="Mastra"
                  language="typescript"
                />
                <CodeBlock
                  value={python}
                  label="OpenAI Agents SDK — Python"
                  language="python"
                />
                <Text muted small>
                  LangGraph, CrewAI and the Claude and OpenAI APIs take the same
                  pair — the URL and an Authorization header — in whatever shape
                  their own MCP client expects.
                </Text>
              </div>
            </TabsContent>
          </Tabs>
        </div>
        {!secret && (
          <div className="absolute inset-0 flex items-center justify-center p-6">
            <div className="bg-card border-border max-w-md space-y-3 border p-5 text-center shadow-sm">
              <Text>
                {issuing
                  ? "Issuing a key…"
                  : "This agent needs a key before it can be connected."}
              </Text>
              <Text muted small className="block">
                A key can only be read at the moment it is issued, so one that
                already exists cannot be shown here.
              </Text>
              <Button disabled={issuing} onClick={onIssue}>
                {issuing ? "Issuing…" : "Issue a key"}
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
