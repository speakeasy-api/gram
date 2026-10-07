import { Button } from "@/components/ui/Button";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { useCallback, useMemo, useState, type JSX } from "react";
import { CodeSnippet } from "@/components/ui/CodeSnippet";
import { ConfigContext } from "@/components/ui/context/config";
import type { Theme } from "@/components/ui/context/theme";
import { WizardStepHeader } from "./WizardChrome";

/**
 * The step that hands the runtime its credential. One command for a machine
 * with a shell, the values themselves for a runtime configured by hand, and
 * the same pair in code for an SDK — each is the same key, shown once.
 */

const KEY_ENV = "GRAM_AGENT_KEY";

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
}: {
  value: string;
  label: string;
  language: string;
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
          the block is the only thing that still reads as one value. */}
      <div aria-label={label}>
        <ConfigContext.Provider value={dark}>
          <CodeSnippet code={value} language={language} copyable wordWrap />
        </ConfigContext.Provider>
      </div>
    </div>
  );
}

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
                <CodeBlock
                  value={command}
                  label="setup command"
                  language="bash"
                />
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
              <Text muted small>
                For a runtime you configure by hand. Each block is the whole
                configuration for that client — paste it, with the key in the
                environment as <code className="text-xs">{KEY_ENV}</code>.
              </Text>
              <div className="space-y-2">
                <span className="text-sm font-medium">
                  API key — shown once
                </span>
                <Copyable value={key} label="API key" />
                <Text muted small>
                  Put it where that runtime reads its secrets. The blocks below
                  name it rather than carry it, so they are safe to paste into a
                  repository.
                </Text>
              </div>
              <CodeBlock
                value={[
                  "# Claude Code",
                  `claude mcp add --transport http gram ${gatewayURL} \\`,
                  `  --header "Authorization: Bearer $${KEY_ENV}"`,
                ].join("\n")}
                label="Claude Code"
                language="bash"
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
                {serverCount} {serverCount === 1 ? "server" : "servers"} sit
                behind that one endpoint. Changing what this agent may reach
                changes what it finds there, without reconnecting.
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
                For agents built on an SDK — Mastra, the Vercel AI SDK,
                LangGraph, CrewAI, the OpenAI Agents SDK. Read the key from your
                secret store; never commit it.
              </Text>
              <CodeBlock
                value={code}
                label="connection snippet"
                language="typescript"
              />
            </div>
          </TabsContent>
        </Tabs>
      </div>
    </div>
  );
}
