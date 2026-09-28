import { useState, type JSX } from "react";
import { ExternalLinkIcon, PlugZapIcon } from "lucide-react";

import { CopyValue } from "@/components/CopyValue";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

export function AdminMcpSetup(): JSX.Element {
  const [origin] = useState(() => window.location.origin);
  if (!origin.startsWith("https://")) {
    return (
      <main className="mx-auto w-full max-w-3xl space-y-3 pb-10">
        <h1 className="text-2xl font-semibold">Connect Admin MCP</h1>
        <p>
          Open this page at the private HTTPS Tailscale dashboard address to
          generate setup instructions. Admin MCP connections cannot use an
          insecure dashboard origin.
        </p>
      </main>
    );
  }

  // The admin dashboard and staff MCP share an origin. Never take the URL
  // from a query parameter or a public app URL.
  const endpoint = `${origin}/admin-mcp`;
  const cursorConfig = JSON.stringify(
    { mcpServers: { "gram-admin": { type: "http", url: endpoint } } },
    null,
    2,
  );

  return (
    <main className="mx-auto w-full max-w-3xl space-y-8 pb-10">
      <header className="space-y-2">
        <div className="flex items-center gap-2">
          <PlugZapIcon className="size-5" aria-hidden="true" />
          <h1 className="text-2xl font-semibold">Connect Admin MCP</h1>
        </div>
        <p className="text-muted-foreground">
          Use Gram’s staff-only tools from an MCP client on your
          tailnet-connected machine. This connection uses your own staff login,
          not an API key.
        </p>
      </header>

      <section className="space-y-3 rounded-lg border p-4">
        <h2 className="font-semibold">Before you connect</h2>
        <ol className="list-inside list-decimal space-y-2 text-sm">
          <li>
            Connect the machine running your MCP client to the company tailnet.
          </li>
          <li>
            Open this dashboard at its private Tailscale address. Use the URL
            below only if this page is loaded from that address, not a public
            alias or a local development server.
          </li>
          <li>
            Choose a local client whose model-provider and data handling are
            approved for staff use. A cloud-hosted agent cannot reach this
            private endpoint unless its runtime has authorised tailnet access.
          </li>
        </ol>
        <div className="flex flex-wrap items-center gap-2 rounded-md bg-muted p-3">
          <span className="text-sm font-medium">Server URL</span>
          <CopyValue
            label="Admin MCP URL"
            value={endpoint}
            className="max-w-full break-all whitespace-normal"
          />
        </div>
      </section>

      <section className="space-y-4">
        <div>
          <h2 className="text-lg font-semibold">Install in your agent</h2>
          <p className="text-muted-foreground text-sm">
            Add a remote Streamable HTTP server. When prompted, sign in with
            your staff account and approve the requested access in the browser.
            A successful connection should list the Admin MCP tools.
          </p>
        </div>
        <Tabs defaultValue="claude" className="min-w-0">
          <TabsList aria-label="MCP client" className="h-auto flex-wrap">
            <TabsTrigger value="claude">Claude Code</TabsTrigger>
            <TabsTrigger value="codex">Codex</TabsTrigger>
            <TabsTrigger value="cursor">Cursor</TabsTrigger>
            <TabsTrigger value="other">Other agents</TabsTrigger>
          </TabsList>
          <TabsContent value="claude" className="space-y-3 pt-3">
            <p className="text-sm">
              Run this in a terminal on your tailnet-connected machine:
            </p>
            <Command
              value={`claude mcp add --transport http --scope user gram-admin ${endpoint}`}
              label="Claude Code command"
            />
            <p className="text-muted-foreground text-sm">
              Open Claude Code and run <code>/mcp</code> to finish
              authentication.
            </p>
          </TabsContent>
          <TabsContent value="codex" className="space-y-3 pt-3">
            <p className="text-sm">
              Run this in a terminal on your tailnet-connected machine:
            </p>
            <Command
              value={`codex mcp add gram-admin --url ${endpoint}`}
              label="Codex command"
            />
            <p className="text-muted-foreground text-sm">
              If prompted to authenticate separately, run{" "}
              <code>codex mcp login gram-admin</code> and complete the browser
              flow.
            </p>
          </TabsContent>
          <TabsContent value="cursor" className="space-y-3 pt-3">
            <p className="text-sm">
              Add this entry to your personal <code>~/.cursor/mcp.json</code>,
              merging it with any servers already there. Avoid committing a
              staff-only connection in a project configuration.
            </p>
            <Command value={cursorConfig} label="Cursor configuration" />
            <p className="text-muted-foreground text-sm">
              Open Cursor’s MCP settings, enable the server, then complete the
              browser sign-in if prompted.
            </p>
          </TabsContent>
          <TabsContent value="other" className="space-y-3 pt-3">
            <p className="text-sm">
              Configure a remote MCP server with Streamable HTTP transport and
              the URL above. Your client must support browser-based OAuth and
              dynamic client registration. Do not paste a staff session token
              into a config file or use an API key as a substitute.
            </p>
            <Command value={endpoint} label="Generic MCP URL" />
          </TabsContent>
        </Tabs>
      </section>

      <p className="text-muted-foreground text-sm">
        The server at <span className="font-mono">{origin}</span> determines
        your actual permissions at connection and on every tool call. Installing
        an MCP server does not grant access by itself. Do not share customer
        data with a model provider unless that use is approved.
      </p>
      <a
        href="https://modelcontextprotocol.io/docs/learn/architecture"
        target="_blank"
        rel="noopener noreferrer"
        className="inline-flex items-center gap-1 text-sm underline underline-offset-4"
      >
        About MCP <ExternalLinkIcon className="size-3" aria-hidden="true" />
      </a>
    </main>
  );
}

function Command({
  value,
  label,
}: {
  value: string;
  label: string;
}): JSX.Element {
  return (
    <div className="rounded-md border bg-muted p-3">
      <CopyValue
        label={label}
        value={value}
        className="min-w-0 whitespace-pre-wrap break-all"
      />
    </div>
  );
}
