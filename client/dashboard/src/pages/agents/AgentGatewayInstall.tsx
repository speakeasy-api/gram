import { Button } from "@/components/ui/Button";
import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { getServerURL } from "@/lib/utils";
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
const KEY_ENV = "GRAM_AGENT_KEY";

/**
 * Whether an endpoint may carry a bearer key. HTTPS everywhere, except
 * loopback, where the request never reaches a network. An unparseable URL is
 * treated as unsafe rather than assumed fine.
 */
function isCredentialSafe(raw: string): boolean {
  let parsed;
  try {
    parsed = new URL(raw);
  } catch {
    return false;
  }
  if (parsed.protocol === "https:") return true;
  return (
    parsed.protocol === "http:" &&
    ["localhost", "127.0.0.1", "[::1]", "::1"].includes(parsed.hostname)
  );
}

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

export function AgentGatewayInstall({
  agentID,
  secret,
}: {
  agentID: string;
  secret: string | null;
}): JSX.Element {
  const [tab, setTab] = useState("url");
  const [copied, setCopied] = useState<string | null>(null);
  // The secret is shown once. When it is gone the snippets still teach the
  // shape, with a placeholder where the key goes.
  const key = secret ?? `<your ${KEY_ENV}>`;
  const url = agentGatewayURL(agentID);
  const all = recipes(url, key, secret);

  // These recipes put a bearer credential on the wire, so a plaintext endpoint
  // would hand the key to anyone on the path. Loopback is exempt: it never
  // leaves the machine, and local development runs there.
  if (!isCredentialSafe(url)) {
    return (
      <div className="space-y-2">
        <h2 className="text-lg font-semibold">Connect your agent</h2>
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
      <h2 className="text-lg font-semibold">Connect your agent</h2>
      <Text muted small>
        One endpoint serving every MCP server this agent can reach. Its
        permissions decide what it finds there, so a change takes effect without
        reconnecting.
      </Text>
      {secret && (
        <div className="space-y-2 pt-2 pb-2">
          <Text small className="font-medium">
            API key — shown once
          </Text>
          <div className="border-border flex items-center gap-2 border p-3">
            <code className="min-w-0 flex-1 break-all text-xs">{secret}</code>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                void navigator.clipboard
                  .writeText(secret)
                  .then(() => setCopied("secret"));
              }}
            >
              {copied === "secret" ? "Copied" : "Copy"}
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
            // tab is showing.
            <TabsContent key={recipe.id} value={recipe.id} forceMount>
              <div className="space-y-3 p-4">
                <pre className="overflow-x-auto text-xs">
                  <code>{recipe.body}</code>
                </pre>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => {
                    void navigator.clipboard
                      .writeText(recipe.body)
                      .then(() => setCopied(recipe.id));
                  }}
                >
                  {copied === recipe.id ? "Copied" : "Copy"}
                </Button>
              </div>
            </TabsContent>
          ))}
        </Tabs>
      </div>
      {!secret && (
        <Text muted small>
          Your key is shown only once. Replace the placeholder with the key you
          saved, or issue a new one.
        </Text>
      )}
    </div>
  );
}
