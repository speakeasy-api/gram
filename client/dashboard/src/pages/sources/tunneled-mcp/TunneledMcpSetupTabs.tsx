import { CodeBlock, type CodeBlockSlot } from "@/components/code";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { cn, tunnelGatewayURL } from "@/lib/utils";
import { Badge } from "@speakeasy-api/moonshine";
import { useEffect, useRef, useState } from "react";

const DEFAULT_MCP_URL = "https://placeholder.net/mcp";
const DEFAULT_SERVICE_VERSION = "1.0.0";

// Sentinels embedded in the snippet text; CodeBlock swaps the shiki token
// containing each one for a live-updating <FlashOnChange> node, so the code
// string itself is stable across keystrokes and never re-tokenizes.
const MCP_URL_SENTINEL = "__SLOT_mcpUrl__";
const SERVICE_VERSION_SENTINEL = "__SLOT_serviceVersion__";

export function TunneledMcpSetupTabs({
  tunnelKey,
  keyPrefix,
  serverName,
}: {
  tunnelKey?: string;
  keyPrefix?: string;
  serverName?: string;
}): JSX.Element {
  const renderedKey = tunnelKey ?? "<YOUR_TUNNEL_KEY>";
  const slug = slugForSnippet(serverName);
  const gateway = tunnelGatewayURL();

  const [mcpUrlDraft, setMcpUrlDraft] = useState(DEFAULT_MCP_URL);
  const [serviceVersionDraft, setServiceVersionDraft] = useState(
    DEFAULT_SERVICE_VERSION,
  );

  const mcpUrl = mcpUrlDraft.trim() || "<YOUR_MCP_URL>";
  const serviceVersion = serviceVersionDraft.trim() || "<YOUR_MCP_VERSION>";

  const kubernetes = `apiVersion: v1
kind: Secret
metadata:
  name: gram-tunnel-key
type: Opaque
stringData:
  TUNNEL_KEY: ${yamlQuote(renderedKey)}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gram-tunnel-${slug}
spec:
  replicas: 1
  selector:
    matchLabels:
      app: gram-tunnel-${slug}
  template:
    metadata:
      labels:
        app: gram-tunnel-${slug}
    spec:
      containers:
        - name: tunnel-agent
          image: ghcr.io/speakeasy-api/gram-tunnel-agent:latest
          env:
            - name: TUNNEL_KEY
              valueFrom:
                secretKeyRef:
                  name: gram-tunnel-key
                  key: TUNNEL_KEY
            - name: TUNNEL_LOCAL_MCP_URL
              value: ${yamlQuote(MCP_URL_SENTINEL)}
            - name: TUNNEL_GATEWAY_URL
              value: ${yamlQuote(gateway)}
            - name: TUNNEL_SERVICE_VERSION
              value: ${yamlQuote(SERVICE_VERSION_SENTINEL)}`;

  const docker = `docker run --rm --name gram-tunnel-${slug} \\
  -e TUNNEL_KEY=${shellQuote(renderedKey)} \\
  -e TUNNEL_LOCAL_MCP_URL='${MCP_URL_SENTINEL}' \\
  -e TUNNEL_GATEWAY_URL=${shellQuote(gateway)} \\
  -e TUNNEL_SERVICE_VERSION='${SERVICE_VERSION_SENTINEL}' \\
  ghcr.io/speakeasy-api/gram-tunnel-agent:latest`;

  const cli = `TUNNEL_GATEWAY_URL=${shellQuote(gateway)} \\
TUNNEL_KEY=${shellQuote(renderedKey)} \\
TUNNEL_LOCAL_MCP_URL='${MCP_URL_SENTINEL}' \\
TUNNEL_SERVICE_VERSION='${SERVICE_VERSION_SENTINEL}' \\
gram tunnel run`;

  // Each slot's display text must reproduce the full shiki token it replaces:
  // YAML emits the quoted value as one token, and bash merges a KEY='value'
  // option argument into a single token (so the Docker slots re-render the key
  // too). In the CLI snippet's assignment position the quoted value stands
  // alone. copyText fills the bare sentinel between the quotes already present
  // in the snippet text, so it is the escaped value without outer quotes.
  const yamlSlots = {
    [MCP_URL_SENTINEL]: yamlSlot(mcpUrl),
    [SERVICE_VERSION_SENTINEL]: yamlSlot(serviceVersion),
  };
  const dockerSlots = {
    [MCP_URL_SENTINEL]: shellSlot(mcpUrl, "TUNNEL_LOCAL_MCP_URL="),
    [SERVICE_VERSION_SENTINEL]: shellSlot(
      serviceVersion,
      "TUNNEL_SERVICE_VERSION=",
    ),
  };
  const cliSlots = {
    [MCP_URL_SENTINEL]: shellSlot(mcpUrl),
    [SERVICE_VERSION_SENTINEL]: shellSlot(serviceVersion),
  };

  const snippetTabs = [
    {
      value: "kubernetes",
      label: "Kubernetes",
      language: "yaml",
      hint: "Deploy the tunnel agent in your cluster. Use your MCP server's in-cluster address, e.g. http://my-mcp.default.svc.cluster.local:3000/mcp.",
      code: kubernetes,
      slots: yamlSlots,
    },
    {
      value: "docker",
      label: "Docker",
      language: "bash",
      hint: "Run the tunnel agent as a container. If your MCP server runs on the Docker host, use http://host.docker.internal:<port> as the address.",
      code: docker,
      slots: dockerSlots,
    },
    {
      value: "cli",
      label: "CLI",
      language: "bash",
      hint: "Run the Gram CLI agent on the same host as your MCP server.",
      code: cli,
      slots: cliSlots,
    },
  ];

  return (
    <div className="rounded-lg border p-6">
      <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
        <div>
          <Type variant="subheading">Connect your MCP server</Type>
          <Type muted small className="mt-1">
            Start a tunnel agent next to the MCP server you already run.
          </Type>
        </div>
        {keyPrefix && (
          <Badge variant="neutral">
            <Badge.Text>{keyPrefix}</Badge.Text>
          </Badge>
        )}
      </div>

      <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[300px_minmax(0,1fr)]">
        <div className="bg-card border-border flex flex-col gap-3 rounded-lg border px-4 py-3 shadow-md lg:sticky lg:top-6 dark:bg-neutral-950">
          <div className="flex flex-col gap-0.5">
            <Type className="font-semibold">Tunnel config</Type>
            <Type muted small>
              Templated into the setup snippets.
            </Type>
          </div>
          <SnippetField
            id="tunnel-config-mcp-url"
            label="MCP server address"
            description="Internal Streamable HTTP endpoint the agent proxies to."
            value={mcpUrlDraft}
            onChange={setMcpUrlDraft}
            placeholder={DEFAULT_MCP_URL}
          />
          <SnippetField
            id="tunnel-config-service-version"
            label="Service version"
            description="Version of the MCP service behind this tunnel."
            value={serviceVersionDraft}
            onChange={setServiceVersionDraft}
            placeholder={DEFAULT_SERVICE_VERSION}
          />
        </div>

        <Tabs defaultValue="kubernetes">
          <TabsList>
            {snippetTabs.map((tab) => (
              <TabsTrigger key={tab.value} value={tab.value}>
                {tab.label}
              </TabsTrigger>
            ))}
          </TabsList>
          {snippetTabs.map((tab) => (
            <TabsContent key={tab.value} value={tab.value} className="mt-4">
              <Type muted small className="mb-3">
                {tab.hint}
              </Type>
              <CodeBlock language={tab.language} slots={tab.slots}>
                {tab.code}
              </CodeBlock>
            </TabsContent>
          ))}
        </Tabs>
      </div>
    </div>
  );
}

function yamlSlot(value: string): CodeBlockSlot {
  return {
    node: <FlashOnChange text={yamlQuote(value)} />,
    copyText: yamlEscape(value),
  };
}

function shellSlot(value: string, tokenPrefix = ""): CodeBlockSlot {
  return {
    node: <FlashOnChange text={`${tokenPrefix}${shellQuote(value)}`} />,
    copyText: shellEscape(value),
  };
}

// Highlights its text with a background fade-in whenever the text changes,
// then fades back out shortly after the last change.
function FlashOnChange({ text }: { text: string }) {
  const [flashing, setFlashing] = useState(false);
  const prevText = useRef(text);

  useEffect(() => {
    if (prevText.current === text) return;
    prevText.current = text;
    setFlashing(true);
    const timer = setTimeout(() => setFlashing(false), 400);
    return () => clearTimeout(timer);
  }, [text]);

  return (
    <span
      className={cn(
        "rounded-xs transition-colors",
        flashing ? "bg-primary/20 duration-150" : "bg-transparent duration-700",
      )}
    >
      {text}
    </span>
  );
}

function SnippetField({
  id,
  label,
  description,
  value,
  onChange,
  placeholder,
}: {
  id: string;
  label: string;
  description: string;
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <Label
        htmlFor={id}
        className="text-muted-foreground font-mono text-xs tracking-wide uppercase"
      >
        {label}
      </Label>
      <Input
        id={id}
        value={value}
        onChange={onChange}
        placeholder={placeholder}
      />
      <Type muted small>
        {description}
      </Type>
    </div>
  );
}

function slugForSnippet(name: string | undefined): string {
  const slug = (name ?? "internal-mcp")
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
  return slug || "internal-mcp";
}

function yamlEscape(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/"/g, '\\"');
}

function yamlQuote(value: string): string {
  return `"${yamlEscape(value)}"`;
}

function shellEscape(value: string): string {
  return value.replace(/'/g, "'\\''");
}

function shellQuote(value: string): string {
  return `'${shellEscape(value)}'`;
}
