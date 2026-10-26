import { Button } from "@/components/ui/Button";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { getServerURL } from "@/lib/utils";
import { openClawInstallMessage } from "./agent-install-message";
import { isCredentialSafe } from "./credentials/gateway";
import { useState, type JSX } from "react";

/**
 * The agent's gateway serves every MCP server its grants reach, so connecting
 * an agent is one URL and one header rather than one entry per server. The
 * snippets below are the same pair rendered in the shape each runtime accepts.
 */
function agentGatewayURL(agentID: string): string {
  // Absolute, always: an agent runs outside the browser, so a path relative to
  // the dashboard is not an address it can dial. getServerURL can be empty
  // when the build left it unset, in which case the current origin is what is
  // serving MCP.
  const base = getServerURL() || window.location.origin;
  return new URL(`/agent-mcp/${agentID}`, base).toString();
}

/** Stands in for the key on the runtimes that read it from the environment. */
const KEY_ENV = "SPEAKEASY_AGENT_KEY";

type Recipe = { id: string; label: string; body: string };

/**
 * Single-quoted for the shell so a key is pasted as written. Keys are
 * generated from a restricted alphabet, but a recipe that is copied straight
 * into a terminal should not depend on that.
 */
function shellQuote(value: string): string {
  return `'${value.replaceAll("'", `'\\''`)}'`;
}

/**
 * The export line that makes the env-var recipes work. Without it they name a
 * variable nothing has set, so a copied recipe connects to nothing. Omitted
 * once the secret is gone, since there is nothing left to export.
 */
function exportLine(secret: string | null): string[] {
  if (!secret) {
    return [`# Set ${KEY_ENV} to the key you saved when it was issued.`, ""];
  }
  return [`export ${KEY_ENV}=${shellQuote(secret)}`, ""];
}

function recipes(url: string, key: string, secret: string | null): Recipe[] {
  return [
    {
      id: "url",
      label: "URL and header",
      body: `${url}\n\nAuthorization: Bearer ${key}`,
    },
    {
      id: "cli",
      label: "CLI",
      body: [
        ...exportLine(secret),
        "# Claude Code",
        `claude mcp add --transport http gram ${url} \\`,
        `  --header "Authorization: Bearer $${KEY_ENV}"`,
        "",
        "# Codex — ~/.codex/config.toml",
        "[mcp_servers.gram]",
        `url = "${url}"`,
        `bearer_token_env_var = "${KEY_ENV}"`,
      ].join("\n"),
    },
    {
      id: "json",
      label: "mcp.json",
      body: JSON.stringify(
        {
          mcpServers: {
            gram: {
              type: "http",
              url,
              headers: { Authorization: `Bearer \${env:${KEY_ENV}}` },
            },
          },
        },
        null,
        2,
      ),
    },
    {
      id: "code",
      label: "Code",
      body: [
        ...exportLine(secret).map((line) => (line ? `// ${line}` : "")),
        "// Mastra, Vercel AI SDK, LangGraph, CrewAI and the OpenAI Agents SDK",
        "// all take the same pair; this is the TypeScript shape.",
        "const gram = {",
        `  url: new URL("${url}"),`,
        "  requestInit: {",
        `    headers: { Authorization: \`Bearer \${process.env.${KEY_ENV}}\` },`,
        "  },",
        "};",
      ].join("\n"),
    },
    {
      id: "api",
      label: "API",
      body: [
        ...exportLine(secret),
        "# Claude API, OpenAI Responses and the Grok API take it per request.",
        "mcp_servers=[{",
        '  "type": "url",',
        `  "url": "${url}",`,
        '  "name": "gram",',
        `  "authorization_token": os.environ["${KEY_ENV}"],`,
        "}]",
      ].join("\n"),
    },
  ];
}

/**
 * Exchanges the key for a single-use install code. The key never goes into the
 * command that way. Until redeemed or expired, the code can retrieve the
 * key and must stay out of chat and shared transcripts.
 */
async function mintInstallCommand(
  gatewayURL: string,
  secret: string,
): Promise<string> {
  const response = await fetch(`${gatewayURL}/install-code`, {
    method: "POST",
    headers: { Authorization: `Bearer ${secret}` },
  });
  if (!response.ok) {
    throw new Error(
      `Could not prepare an install command (${response.status})`,
    );
  }
  const { code } = (await response.json()) as { code: string };
  // This value is pasted into a shell. It is minted by our own server, but a
  // command built by interpolation is only safe if what goes into it cannot
  // carry shell syntax, so that is checked here rather than assumed upstream.
  if (!/^[A-Za-z0-9_-]+$/.test(code)) {
    throw new Error("Could not prepare an install command (malformed code)");
  }
  const base = new URL(gatewayURL);
  return `curl -fsSL ${base.origin}/agent-mcp/install/${code} | sh`;
}

export function AgentGatewayInstall({
  agentID,
  secret,
}: {
  agentID: string;
  secret: string | null;
}): JSX.Element {
  const [tab, setTab] = useState("url");
  const [copied, setCopied] = useState<string | null>(null);
  const [copyFailed, setCopyFailed] = useState<string | null>(null);
  const [command, setCommand] = useState<string | null>(null);
  const [commandError, setCommandError] = useState<string | null>(null);
  const [minting, setMinting] = useState(false);
  // The secret is shown once. When it is gone the snippets still teach the
  // shape, with a placeholder where the key goes.
  const key = secret ?? `<your ${KEY_ENV}>`;
  const url = agentGatewayURL(agentID);
  const all = recipes(url, key, secret);

  // The clipboard can refuse (permissions, an insecure context), and the key
  // and install code are shown once, so a refusal has to be visible.
  const copy = async (id: string, text: string) => {
    setCopyFailed(null);
    setCopied(null);
    try {
      if (!navigator.clipboard) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(text);
      setCopied(id);
    } catch {
      setCopyFailed(id);
    }
  };
  const copyLabel = (id: string) =>
    copied === id ? "Copied" : copyFailed === id ? "Copy failed" : "Copy";

  // These recipes put a bearer credential on the wire, so a plaintext endpoint
  // would hand the key to anyone on the path. Loopback is exempt: it never
  // leaves the machine, and local development runs there.
  if (!isCredentialSafe(url)) {
    return (
      <div className="space-y-2">
        <Text role="alert">
          This deployment serves MCP over plaintext HTTP at {url}. Connection
          instructions are withheld because they would send the agent key
          unencrypted. Configure an HTTPS server URL and reload.
        </Text>
      </div>
    );
  }

  return (
    <div className="space-y-2">
      {/* No heading of its own: this renders inside a titled section on the
          agent page and under the wizard's own heading when a key is issued. */}
      <Text muted small>
        One endpoint serving every MCP server this agent can reach. Its
        permissions decide what it finds there, so a change takes effect without
        reconnecting.
      </Text>
      {secret && (
        <div className="space-y-2 pt-2 pb-2">
          <Text small className="font-medium">
            One command
          </Text>
          {command && (
            <div className="border-border flex items-center gap-2 border p-3">
              <code className="min-w-0 flex-1 break-all text-xs">
                {command}
              </code>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void copy("command", command)}
              >
                {copyLabel("command")}
              </Button>
            </div>
          )}
          {/* Always available: a code is spent on first fetch and expires in
              15 minutes, so a delayed or failed install needs a fresh one. */}
          <Button
            variant="secondary"
            size="sm"
            disabled={minting}
            onClick={() => {
              setMinting(true);
              setCommandError(null);
              mintInstallCommand(url, secret)
                .then(setCommand)
                .catch((error: Error) => setCommandError(error.message))
                .finally(() => setMinting(false));
            }}
          >
            {minting
              ? "Preparing…"
              : command
                ? "Generate a new command"
                : "Generate install command"}
          </Button>
          <Text muted small>
            Connects Claude Code, Codex and OpenClaw on a machine in one step.
            On OpenClaw it also installs Speakeasy hooks when this agent has a
            project. Session attribution depends on the active hooks credential.
            Models OpenClaw routes through the Claude CLI bypass those hooks and
            are recorded only if Claude Code hooks are installed too. The
            command carries a single-use code that can retrieve the key for 15
            minutes. Run it in a trusted shell; do not paste it into chat.
          </Text>
          {command && (
            <Text muted small>
              Piping to a shell runs whatever this deployment returns. To read
              it first, fetch it to a file and run that instead — but the code
              is spent by the fetch, so generate a new command afterwards. The
              saved file carries the key: treat it as a secret and delete it
              once you have run it.{" "}
              <code className="text-xs">
                {command.replace(" | sh", " -o gram-install.sh")}
              </code>
            </Text>
          )}
          {command && (
            <>
              <Text small className="font-medium">
                After installation, ask OpenClaw to check
              </Text>
              <div className="border-border flex items-start gap-2 border p-3">
                <pre className="min-w-0 flex-1 whitespace-pre-wrap break-all text-xs">
                  {openClawInstallMessage()}
                </pre>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() =>
                    void copy("openclaw", openClawInstallMessage())
                  }
                >
                  {copyLabel("openclaw")}
                </Button>
              </div>
              <Text muted small>
                Run the command yourself and restart the OpenClaw Gateway before
                sending this message. It contains no install code or key.
              </Text>
            </>
          )}
          {commandError && (
            <Text role="alert" small>
              {commandError}
            </Text>
          )}
          <Text small className="font-medium">
            API key — shown once
          </Text>
          <div className="border-border flex items-center gap-2 border p-3">
            <code className="min-w-0 flex-1 break-all text-xs">{secret}</code>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => void copy("secret", secret)}
            >
              {copyLabel("secret")}
            </Button>
          </div>
          <Text muted small>
            Store it before you leave this page. The snippets below already
            carry it; copy one of those to connect a runtime directly.
          </Text>
        </div>
      )}
      <div className="border-border border">
        <Tabs value={tab} onValueChange={setTab} className="gap-0">
          <div className="border-border bg-muted/30 border-b px-4">
            <PageTabsList>
              {all.map((recipe) => (
                <PageTabsTrigger key={recipe.id} value={recipe.id}>
                  {recipe.label}
                </PageTabsTrigger>
              ))}
            </PageTabsList>
          </div>
          {all.map((recipe) => (
            // forceMount: every recipe stays in the DOM so switching tabs does
            // not re-render a snippet, and so the copy target exists whichever
            // tab is showing. A force-mounted panel is not hidden for us, so
            // the inactive ones are hidden here or every recipe renders at once.
            <TabsContent
              key={recipe.id}
              value={recipe.id}
              forceMount
              className="data-[state=inactive]:hidden"
            >
              <div className="space-y-3 p-4">
                <pre className="overflow-x-auto text-xs">
                  <code>{recipe.body}</code>
                </pre>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => void copy(recipe.id, recipe.body)}
                >
                  {copyLabel(recipe.id)}
                </Button>
              </div>
            </TabsContent>
          ))}
        </Tabs>
      </div>
      {/* Every recipe above puts the key in an Authorization header, which is
          the shape a runtime also uses for a model provider or another MCP
          server. The key authenticates to Speakeasy and nowhere else, so where
          it may go has to be said next to the snippets that carry it. */}
      <Text muted small>
        This key is a bearer credential for Speakeasy endpoints only. Send it to
        the gateway URL above and to nothing else — never to a model provider,
        and never on to an upstream MCP server.
      </Text>
      {copyFailed && (
        <Text role="alert" small>
          The browser did not allow copying to the clipboard. Select the text
          and copy it manually.
        </Text>
      )}
      {!secret && (
        <Text muted small>
          Your key is shown only once. Replace the placeholder with the key you
          saved, or issue a new one.
        </Text>
      )}
    </div>
  );
}
