import { Button } from "@/components/ui/Button";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useState, type JSX } from "react";
import { WizardStepHeader } from "./WizardChrome";

/**
 * The step that hands the runtime its credential. One command for a machine
 * with a shell, the values themselves for a runtime configured by hand, and
 * the same pair in code for an SDK — each is the same key, shown once.
 */

const KEY_ENV = "GRAM_AGENT_KEY";

function Copyable({
  value,
  label,
  mono = true,
}: {
  value: string;
  label: string;
  mono?: boolean;
}): JSX.Element {
  const [copied, setCopied] = useState(false);
  return (
    <div className="border-border flex items-start gap-2 border p-3">
      <code
        className={`min-w-0 flex-1 break-all text-xs ${mono ? "" : "font-sans"}`}
      >
        {value}
      </code>
      <Button
        size="sm"
        variant="secondary"
        aria-label={`Copy ${label}`}
        onClick={() => {
          void navigator.clipboard.writeText(value).then(() => setCopied(true));
        }}
      >
        {copied ? "Copied" : "Copy"}
      </Button>
    </div>
  );
}

export function StepProvision({
  command,
  commandError,
  minting,
  onRegenerate,
  secret,
  gatewayURL,
  serverCount,
}: {
  command: string | null;
  commandError: string | null;
  minting: boolean;
  onRegenerate: () => void;
  secret: string | null;
  gatewayURL: string;
  serverCount: number;
}): JSX.Element {
  const [tab, setTab] = useState("one-line");
  const key = secret ?? `<your ${KEY_ENV}>`;
  const code = [
    `// Read the key from your secret store; never commit it.`,
    `const gram = {`,
    `  url: new URL("${gatewayURL}"),`,
    `  requestInit: {`,
    `    headers: { Authorization: \`Bearer \${process.env.${KEY_ENV}}\` },`,
    `  },`,
    `};`,
  ].join("\n");

  return (
    <div className="space-y-5">
      <WizardStepHeader
        title="Connect the agent"
        description="Pick how its runtime will be configured."
      />
      <div className="border-border border">
        <Tabs value={tab} onValueChange={setTab} className="gap-0">
          <div className="border-border bg-muted/30 border-b px-4">
            <PageTabsList>
              <PageTabsTrigger value="one-line">One-line setup</PageTabsTrigger>
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
                For runtimes with a shell — Claude Code, Codex, Cursor, CI
                runners. Run once on the machine the agent lives on.
              </Text>
              {command ? (
                <Copyable value={command} label="setup command" />
              ) : (
                <Text muted small>
                  {minting
                    ? "Preparing the setup command…"
                    : "No setup command yet."}
                </Text>
              )}
              <div className="space-y-2">
                <span className="text-muted-foreground font-mono text-[10px] tracking-[0.08em] uppercase">
                  The script
                </span>
                <ol className="space-y-1 text-sm">
                  {[
                    "Exchanges the single-use setup code for this agent's API key.",
                    `Configures the MCP clients it finds on PATH with the agent's gateway (${serverCount} ${serverCount === 1 ? "server" : "servers"}).`,
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
              <Text muted small>
                The setup link is single-use and expires in 15 minutes.{" "}
                <button
                  type="button"
                  className="underline"
                  disabled={minting || !secret}
                  onClick={onRegenerate}
                >
                  Regenerate
                </button>
              </Text>
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
              <div className="space-y-2">
                <span className="text-sm font-medium">
                  API key — shown once
                </span>
                <Copyable value={key} label="API key" />
              </div>
              <div className="space-y-2">
                <span className="text-sm font-medium">Header</span>
                <Copyable
                  value={`Authorization: Bearer $${KEY_ENV}`}
                  label="header"
                />
              </div>
              <div className="space-y-2">
                <span className="text-sm font-medium">
                  Gateway URL · {serverCount}{" "}
                  {serverCount === 1 ? "server" : "servers"} behind one endpoint
                </span>
                <Copyable value={gatewayURL} label="gateway URL" />
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
                For agents built on an SDK — Mastra, the Vercel AI SDK,
                LangGraph, CrewAI, the OpenAI Agents SDK. Read the key from your
                secret store; never commit it.
              </Text>
              <pre className="border-border overflow-x-auto border p-3 text-xs">
                <code>{code}</code>
              </pre>
            </div>
          </TabsContent>
        </Tabs>
      </div>
    </div>
  );
}
