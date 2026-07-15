import { CodeBlock } from "@/components/code";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import { tunnelGatewayURL } from "@/lib/utils";
import { Badge } from "@speakeasy-api/moonshine";
import { useState } from "react";

const DEFAULT_MCP_URL = "http://localhost:3000/mcp";
const DEFAULT_SERVICE_VERSION = "1.0.0";

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
              value: ${yamlQuote(mcpUrl)}
            - name: TUNNEL_GATEWAY_URL
              value: ${yamlQuote(gateway)}
            - name: TUNNEL_SERVICE_VERSION
              value: ${yamlQuote(serviceVersion)}`;

  const docker = `docker run --rm --name gram-tunnel-${slug} \\
  -e TUNNEL_KEY=${shellQuote(renderedKey)} \\
  -e TUNNEL_LOCAL_MCP_URL=${shellQuote(mcpUrl)} \\
  -e TUNNEL_GATEWAY_URL=${shellQuote(gateway)} \\
  -e TUNNEL_SERVICE_VERSION=${shellQuote(serviceVersion)} \\
  ghcr.io/speakeasy-api/gram-tunnel-agent:latest`;

  const cli = `TUNNEL_GATEWAY_URL=${shellQuote(gateway)} \\
TUNNEL_KEY=${shellQuote(renderedKey)} \\
TUNNEL_LOCAL_MCP_URL=${shellQuote(mcpUrl)} \\
TUNNEL_SERVICE_VERSION=${shellQuote(serviceVersion)} \\
gram tunnel run`;

  const snippetTabs = [
    {
      value: "kubernetes",
      label: "Kubernetes",
      language: "yaml",
      hint: "Deploy the tunnel agent in your cluster. Use your MCP server's in-cluster address, e.g. http://my-mcp.default.svc.cluster.local:3000/mcp.",
      code: kubernetes,
    },
    {
      value: "docker",
      label: "Docker",
      language: "bash",
      hint: "Run the tunnel agent as a container. If your MCP server runs on the Docker host, use http://host.docker.internal:<port> as the address.",
      code: docker,
    },
    {
      value: "cli",
      label: "CLI",
      language: "bash",
      hint: "Run the Gram CLI agent on the same host as your MCP server.",
      code: cli,
    },
  ];

  return (
    <div className="rounded-lg border p-6">
      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
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

      <div className="bg-muted/30 mb-5 rounded-md border p-4">
        <Type variant="subheading" className="mb-1">
          Tunnel config
        </Type>
        <Type muted small className="mb-4 max-w-3xl">
          Describe the MCP server the tunnel agent should proxy to. The values
          are templated into the setup snippets below.
        </Type>
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
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
            <CodeBlock language={tab.language}>{tab.code}</CodeBlock>
          </TabsContent>
        ))}
      </Tabs>
    </div>
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
    <div className="min-w-0">
      <Label htmlFor={id} className="mb-2">
        {label}
      </Label>
      <Input
        id={id}
        value={value}
        onChange={onChange}
        placeholder={placeholder}
      />
      <Type muted small className="mt-1">
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

function yamlQuote(value: string): string {
  return `"${value.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, "'\\''")}'`;
}
