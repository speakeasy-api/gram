import { AIToolIcon } from "@/components/ai-tools/AIToolIcon";
import { InstallSteps, type InstallStep } from "@/components/install-steps";
import { CopyButton } from "@/components/ui/CopyButton";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@/components/ui/Sheet";
import { Text } from "@/components/ui/Text";
import { useState } from "react";

const CLIENTS = [
  {
    id: "claude-code",
    icon: "claude-code",
    name: "Claude Code",
    docs: "https://code.claude.com/docs/en/mcp",
  },
  {
    id: "codex",
    icon: "codex",
    name: "Codex",
    docs: "https://developers.openai.com/codex/mcp/",
  },
  {
    id: "claude-desktop",
    icon: "claude",
    name: "Claude Desktop",
    docs: "https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp",
  },
  {
    id: "claude-chat",
    icon: "claude",
    name: "Claude Chat",
    docs: "https://support.claude.com/en/articles/11175166-get-started-with-custom-connectors-using-remote-mcp",
  },
] as const;

type Client = (typeof CLIENTS)[number];

function stepsFor(client: Client, url: string): InstallStep[] {
  // URLs come from the API; quote them as shell arguments rather than code.
  const quoted = `'${url.replaceAll("'", "'\\''")}'`;
  if (client.id === "claude-code")
    return [
      {
        title: "Add the gateway",
        code: `claude mcp add --transport http --scope user speakeasy-org ${quoted}`,
      },
      {
        title: "Sign in",
        description:
          "Open Claude Code, run /mcp, select speakeasy-org, and authenticate with your organization account.",
      },
      {
        title: "Check your connection",
        description:
          "Confirm the gateway is connected in /mcp, then ask Claude to find a tool from one of your assigned servers.",
      },
    ];
  if (client.id === "codex")
    return [
      {
        title: "Add the gateway",
        code: `codex mcp add speakeasy-org --url ${quoted}`,
      },
      {
        title: "Sign in",
        code: "codex mcp login speakeasy-org",
        description:
          "Complete the browser sign-in with your organization account.",
      },
      {
        title: "Check your connection",
        description:
          "Start Codex and use /mcp to confirm the gateway is available.",
      },
    ];
  return [
    {
      title: "Add a custom connector",
      description: `In ${client.name}, open Connectors and add a custom connector named Speakeasy Org Gateway. For Team and Enterprise, an owner first adds it in Organization settings → Connectors.`,
    },
    {
      title: "Use the gateway URL",
      children: (
        <div className="flex min-w-0 items-center gap-3">
          <code className="min-w-0 flex-1 break-all text-xs">{url}</code>
          <CopyButton text={url} tooltip="Copy gateway URL" />
        </div>
      ),
    },
    {
      title: "Connect your account",
      description:
        "Choose Connect and finish sign-in using your own organization account. Enable the connector in a conversation to use your assigned servers.",
    },
  ];
}

export function GatewayOnboarding({ url }: { url?: string }): JSX.Element {
  const [client, setClient] = useState<Client | null>(null);
  return (
    <section className="space-y-4">
      <Text variant="subheading">Connect your AI client</Text>
      <Text muted small>
        {url
          ? "Each member uses the same URL and signs in with their own account."
          : "Enable Org Gateway to get your connection URL. You can preview the setup steps below."}
      </Text>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {CLIENTS.map((target) => (
          <button
            key={target.id}
            type="button"
            onClick={() => setClient(target)}
            className="border-border hover:bg-muted flex flex-col items-center gap-3 border p-5 text-sm"
          >
            <AIToolIcon
              targetId={target.icon}
              displayName={target.name}
              className="size-8"
            />
            <span>{target.name}</span>
          </button>
        ))}
      </div>
      <Sheet
        open={client !== null}
        onOpenChange={(open) => {
          if (!open) setClient(null);
        }}
      >
        <SheetContent className="overflow-y-auto sm:max-w-xl">
          {client && (
            <>
              <SheetHeader>
                <SheetTitle>Connect {client.name}</SheetTitle>
                <SheetDescription>
                  Sign in as yourself to use your assigned MCP servers.
                </SheetDescription>
              </SheetHeader>
              <div className="space-y-5 px-4 pb-6">
                {!url && (
                  <Text small muted>
                    These are preview instructions. Enable the gateway first to
                    get your URL.
                  </Text>
                )}
                <InstallSteps
                  steps={stepsFor(client, url ?? "<GATEWAY_URL>")}
                />
                <Text small muted>
                  If the client refuses sign-in, ask your admin to check that it
                  is allowed by your organization’s IDP and SSO settings.
                </Text>
                <a
                  className="text-sm underline"
                  href={client.docs}
                  target="_blank"
                  rel="noreferrer"
                >
                  Read {client.name} connection documentation
                </a>
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </section>
  );
}
