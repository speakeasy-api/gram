import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { formatRelativeTime } from "@/lib/dates";
import { cn } from "@/lib/utils";
import type { AgentRuntimeGatewayUnavailableReason } from "@gram/client/models/components/agentruntimegateway.js";
import { useState, type JSX } from "react";
import { CodeBlock } from "./CodeBlock";
import { PanelTab, PanelTabs } from "./PanelTabs";
import { WizardStepHeader } from "./WizardChrome";
import { isCredentialSafe } from "./gateway";

/** The ways the agent's runtime can be handed its key. */
const INSTALL_TABS = [
  { value: "manual", label: "Manual" },
  { value: "programmatic", label: "Programmatic" },
];

/**
 * The step that hands the runtime its credential: values for a runtime
 * configured by hand, or the same pair in code for an SDK. The key is shown once.
 */

const KEY_ENV = "SPEAKEASY_AGENT_KEY";

/** A key the agent already holds, which this step cannot show again. */
export type ExistingAgentKey = {
  /** When a runtime last used it; absent for a key nothing has used. */
  lastUsedAt?: Date;
};

/**
 * What covers the step while no key is on screen. An agent that already has
 * one is usually connected, so saying it "needs a key" reads as if it were
 * not: the only thing missing is a key to show, and a new one is for a new
 * runtime. While the agent's keys are unknown the copy claims neither.
 */
function keyCover(existingKey: ExistingAgentKey | null | undefined): {
  title: string;
  detail: string;
  action: string;
} {
  if (existingKey === undefined) {
    return {
      title: "A key that already exists cannot be shown here.",
      detail:
        "A key can only be read at the moment it is issued. Issue one to connect a runtime: any key the agent already holds keeps working.",
      action: "Issue a key",
    };
  }
  if (existingKey === null) {
    return {
      title: "This agent needs a key before it can be connected.",
      detail:
        "A key can only be read at the moment it is issued, so one that already exists cannot be shown here.",
      action: "Issue a key",
    };
  }
  const lastUsed = formatRelativeTime(existingKey.lastUsedAt ?? null);
  if (!lastUsed) {
    return {
      title: "This agent already has a key.",
      detail:
        "A key can only be read at the moment it is issued, so it cannot be shown here. Nothing has used it yet: issue another only if that one was lost.",
      action: "Issue another key",
    };
  }
  return {
    title: `This agent is already connected. Its key was last used ${lastUsed}.`,
    detail:
      "A key can only be read at the moment it is issued, so it cannot be shown here. Issue another only to connect a new runtime: the current one keeps working.",
    action: "Issue another key",
  };
}

export type GatewayUnavailableReason =
  | AgentRuntimeGatewayUnavailableReason
  | "loading"
  | "lookup_failed";

export function StepInstall({
  onIssue,
  issuing,
  secret,
  existingKey,
  gatewayURL,
  gatewayUnavailableReason,
  serverCount,
}: {
  /** Issue a key, for a step reached without one to show. */
  onIssue: () => void;
  issuing: boolean;
  secret: string | null;
  /**
   * The agent's live key when it has one, `null` when it is known to have
   * none, and `undefined` while that is unknown.
   */
  existingKey: ExistingAgentKey | null | undefined;
  gatewayURL: string;
  gatewayUnavailableReason?: GatewayUnavailableReason;
  /** `"all"` where the agent's reach is a wildcard rather than a list. */
  serverCount: number | "all";
}): JSX.Element {
  const [tab, setTab] = useState("manual");
  // Every recipe here puts a bearer key on the wire, and the key itself is on
  // screen beside them, so a plaintext endpoint would hand it to anyone on
  // the path. Loopback is exempt: it never leaves the machine, and local
  // development runs there. Empty means no agent exists yet, which has no
  // endpoint to judge — the panel below is covered in that case anyway.
  const unsafeTransport = gatewayURL !== "" && !isCredentialSafe(gatewayURL);
  // Counting is not available for a wildcard, and a count taken from today's
  // inventory would be a number that quietly changes behind the endpoint.
  const reachClause =
    serverCount === "all"
      ? "Every server sits"
      : `${serverCount} ${serverCount === 1 ? "server sits" : "servers sit"}`;
  const key = secret ?? `<your ${KEY_ENV}>`;
  const cover = keyCover(existingKey);
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

  if (gatewayUnavailableReason) {
    const unavailableByReason: Record<
      string,
      { description: string; action: string }
    > = {
      loading: {
        description: "Checking the agent's project gateway.",
        action: "Connection instructions will appear when this check finishes.",
      },
      lookup_failed: {
        description: "The agent's project gateway could not be checked.",
        action:
          "Reload the page and try again. No connection instructions were shown.",
      },
      project_assignment_required: {
        description: "This agent is not assigned to a project.",
        action: "Assign the agent to a project before setting up its runtime.",
      },
      machine_access_disabled: {
        description: "Machine access to this project gateway is disabled.",
        action:
          "Ask an administrator to enable project gateway machine access, then reload this page.",
      },
      no_organization_issuer: {
        description:
          "The project gateway needs an organization sign-in issuer.",
        action:
          "Configure the organization's sign-in issuer, then reload this page.",
      },
      multiple_organization_issuers: {
        description:
          "The project gateway has more than one organization sign-in issuer.",
        action:
          "Choose one organization sign-in issuer, then reload this page.",
      },
      custom_domain_only: {
        description:
          "The project gateway is available only on a custom domain.",
        action: "Finish activating the custom domain, then reload this page.",
      },
      organization_disabled: {
        description: "The organization gateway is disabled.",
        action: "Enable the organization gateway, then reload this page.",
      },
      turned_off: {
        description: "The project gateway is turned off.",
        action: "Turn on the project gateway, then reload this page.",
      },
    };
    const unavailable = unavailableByReason[gatewayUnavailableReason] ?? {
      description: "The agent's project gateway is unavailable.",
      action:
        "Ask an administrator to review the gateway configuration, then reload this page.",
    };
    return (
      <div className="space-y-5">
        <WizardStepHeader
          title="Connect the agent"
          description={unavailable.description}
        />
        <Text role="alert">{unavailable.action}</Text>
      </div>
    );
  }

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
        <PanelTabs
          aria-label="Setup method"
          value={tab}
          onValueChange={setTab}
          tabs={INSTALL_TABS}
          className={cn(
            !secret && "pointer-events-none select-none blur-[2px]",
          )}
          aria-hidden={!secret}
          inert={!secret ? true : undefined}
        >
          <PanelTab value="manual">
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
                Put it where that runtime reads its secrets. The blocks below
                name it rather than carry it, so they are safe to paste into a
                repository.
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
                // named `speakeasy` consistently with the hosted gateway.
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
              {reachClause} behind that one endpoint. Changing what this agent
              may reach changes what it finds there, without reconnecting.
            </Text>
          </PanelTab>

          <PanelTab value="programmatic">
            <Text muted small>
              The endpoint speaks MCP over HTTP, so any SDK that takes an MCP
              server takes this one. Read the key from the environment; never
              commit it.
            </Text>
            <CodeBlock
              value={vercel}
              label="Vercel AI SDK"
              language="typescript"
            />
            <CodeBlock value={mastra} label="Mastra" language="typescript" />
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
          </PanelTab>
        </PanelTabs>
        {!secret && (
          <div className="absolute inset-0 flex items-center justify-center p-6">
            <div className="bg-card border-border max-w-md space-y-3 border p-5 text-center shadow-sm">
              <Text>{issuing ? "Issuing a key…" : cover.title}</Text>
              <Text muted small className="block">
                {cover.detail}
              </Text>
              <Button disabled={issuing} onClick={onIssue}>
                {issuing ? "Issuing…" : cover.action}
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
