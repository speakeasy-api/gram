import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import {
  useCallback,
  useMemo,
  useState,
  type CSSProperties,
  type JSX,
} from "react";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { ConfigContext } from "@/components/ui/context/config";
import type { Theme } from "@/components/ui/context/theme";
import { WizardStepHeader } from "./WizardChrome";
import { isCredentialSafe } from "./gateway";
import { deviceAgentManagedConfig } from "./deviceAgentConfig";
import {
  MANAGED_CONFIG_PATHS,
  SANDBOX_INSTALL_SCRIPT_URL,
} from "@/pages/device-agent/release-paths";

/**
 * The step that hands the runtime its credential. One command for a machine
 * with a shell, the values themselves for a runtime configured by hand, and
 * the same pair in code for an SDK — each is the same key, shown once.
 */

const KEY_ENV = "SPEAKEASY_AGENT_KEY";

// A headless machine is a Linux box: a VPS, a CI runner, a sandbox.
const LINUX_MANAGED_CONFIG_PATH =
  MANAGED_CONFIG_PATHS.find((row) => row.os === "Linux")?.path ??
  "/etc/speakeasy/managed.json";

/** The origin of an endpoint, or empty when there is no endpoint yet. */
function originOf(url: string): string {
  try {
    return new URL(url).origin;
  } catch {
    return "";
  }
}

/**
 * A code block, syntax highlighted and dark whatever the app theme is: these
 * are things to paste into a terminal or a config file, and reading them as
 * code is the point. The design system's snippet follows the app theme, so
 * the theme it reads is overridden here rather than forked.
 */
function CodeBlock({
  value,
  label,
  language,
  breakAnywhere = false,
}: {
  value: string;
  label: string;
  language: string;
  /**
   * Break mid-token. Wrapping is by whitespace, so a single long value — a
   * key — overflows instead, and takes the copy button off the edge with it.
   */
  breakAnywhere?: boolean;
}): JSX.Element {
  const setTheme = useCallback(() => undefined, []);
  const dark = useMemo(
    () => ({ theme: "dark" as Theme, setTheme }),
    [setTheme],
  );
  return (
    <div className="space-y-2">
      <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
        {label}
      </span>
      {/* Named, because highlighting splits the code into token elements and
          the block is the only thing that still reads as one value.
          The dark tokens are declared on :root.dark, which nothing nested can
          match, so the snippet's surface stays light however its highlighting
          is themed. These are the three it paints with, at their dark values,
          scoped to this block. */}
      <div
        aria-label={label}
        style={
          {
            "--card": "var(--color-neutral-900)",
            "--muted": "var(--color-neutral-900)",
            "--text-body": "var(--color-neutral-200)",
          } as CSSProperties
        }
      >
        <ConfigContext.Provider value={dark}>
          <CodeSnippet
            code={value}
            language={language}
            copyable
            // Every block carries a copy button, so each one says which.
            copyLabel={`Copy ${label}`}
            wordWrap
            snippetClassName={breakAnywhere ? "break-all" : undefined}
          />
        </ConfigContext.Provider>
      </div>
    </div>
  );
}

export function StepInstall({
  command,
  commandError,
  minting,
  onRegenerate,
  onIssue,
  issuing,
  secret,
  agentID,
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
  /** Empty until the agent exists. */
  agentID: string;
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
  // The gateway is served by the same server the device agent must reach, so
  // its origin is the control plane, wherever this dashboard is deployed.
  const managedConfig = deviceAgentManagedConfig({
    mcpCredential: key,
    agentID,
    serverOrigin: originOf(gatewayURL),
  });
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
                <PageTabsTrigger value="device-agent">
                  Device Agent
                </PageTabsTrigger>
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
                  For runtimes with a shell — Claude Code, Codex, Cursor, CI
                  runners. Run once on the machine the agent lives on.
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
                      "Exchanges the single-use setup code for this agent's API key.",
                      `Configures the MCP clients it finds on PATH with the agent's gateway (${reachPhrase}).`,
                      "Leaves the key out of your shell history: the code dies on first use.",
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
                    The setup link is single-use and expires in 15 minutes.{" "}
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
                    read it first, fetch it to a file and run that instead — the
                    code is spent by the fetch, so regenerate afterwards. The
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
              value="device-agent"
              forceMount
              className="data-[state=inactive]:hidden"
            >
              <div className="space-y-4 p-4">
                <Text muted small>
                  For a machine with no person at it — a VPS, CI runner or
                  sandbox. The Speakeasy device agent sets up MCP for Claude
                  Code, Codex and Cursor on that machine, and keeps this agent
                  identity&apos;s key out of their config files.
                </Text>
                <CodeBlock
                  value={`curl -fsSL ${SANDBOX_INSTALL_SCRIPT_URL} | sudo sh`}
                  label="1 · Install the device agent — Linux"
                  language="shell"
                />
                <div className="space-y-2">
                  <CodeBlock
                    value={managedConfig}
                    label="2 · Save as managed.json — shown once"
                    language="json"
                    breakAnywhere
                  />
                  <CodeBlock
                    value={`sudo install -D -o root -g <group> -m 0640 managed.json ${LINUX_MANAGED_CONFIG_PATH} && rm managed.json`}
                    label={`Move it to ${LINUX_MANAGED_CONFIG_PATH}`}
                    language="shell"
                  />
                  <Text muted small>
                    Replace <code className="text-xs">&lt;group&gt;</code> with
                    the private group of the account that runs the AI tools — on
                    most distributions, the group named after that account. Only
                    members of that group can read the file.
                  </Text>
                </div>
                <div className="space-y-2">
                  <CodeBlock
                    value="speakeasyd sync --once"
                    label="3 · Configure the tools — as the account that runs them"
                    language="shell"
                  />
                  <Text muted small>
                    Then <code className="text-xs">claude mcp list</code> should
                    show <code className="text-xs">speakeasy-gateway</code> as
                    connected.
                  </Text>
                </div>
                <div className="space-y-2">
                  <span className="text-eyebrow">What this sets up</span>
                  <Text muted small className="block">
                    MCP access only, to {reachPhrase} through this agent
                    identity&apos;s permissions. Plugin sync and hooks are not
                    included yet.
                  </Text>
                </div>
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
