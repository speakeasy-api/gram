import { CodeBlock, type CodeBlockSlot } from "@/components/code";
import { McpSidebarInfoLabel } from "@/components/mcp-sidebar-nav-shell";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
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

type SetupMode = "existing" | "new";

type Platform = "kubernetes" | "docker" | "cli";

type SnippetTab = {
  value: string;
  label: string;
  language: string;
  hint: string;
  code: string;
  slots: Record<string, CodeBlockSlot>;
};

type SnippetContext = {
  renderedKey: string;
  slug: string;
  gateway: string;
  mcpUrl: string;
  serviceVersion: string;
};

export function TunneledMcpSetupTabs({
  tunnelKey,
  keyPrefix,
  serverName,
}: {
  tunnelKey?: string;
  keyPrefix?: string;
  serverName?: string;
}): JSX.Element {
  const [mode, setMode] = useState<SetupMode>("existing");
  const [platform, setPlatform] = useState<Platform>("kubernetes");
  const [mcpUrlDraft, setMcpUrlDraft] = useState("");
  const [serviceVersionDraft, setServiceVersionDraft] = useState("");

  const ctx: SnippetContext = {
    renderedKey: tunnelKey ?? "<YOUR_TUNNEL_KEY>",
    slug: slugForSnippet(serverName),
    gateway: tunnelGatewayURL(),
    mcpUrl: mcpUrlDraft.trim() || DEFAULT_MCP_URL,
    serviceVersion: serviceVersionDraft.trim() || DEFAULT_SERVICE_VERSION,
  };

  const snippetTabs =
    mode === "existing" ? existingServerTabs(ctx) : newServerTabs(ctx);
  const activeSnippet =
    snippetTabs.find((tab) => tab.value === platform) ?? snippetTabs[0]!;

  const handleModeChange = (value: string) => {
    if (value === "existing" || value === "new") setMode(value);
  };

  const handlePlatformChange = (value: string) => {
    if (value === "kubernetes" || value === "docker" || value === "cli") {
      setPlatform(value);
    }
  };

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
          <Type className="font-semibold">Tunnel config</Type>
          <ConfigGroup label="Tunnel endpoint">
            <Tabs value={mode} onValueChange={handleModeChange}>
              <TabsList className="w-full">
                <TabsTrigger value="existing">Existing server</TabsTrigger>
                <TabsTrigger value="new">New server</TabsTrigger>
              </TabsList>
            </Tabs>
            <Type muted small>
              {mode === "existing"
                ? "Point the tunnel agent at an MCP server you already run. Values are templated into the setup snippet."
                : "Deploy a sample hello-world MCP server together with the tunnel agent to try the tunnel end to end."}
            </Type>
          </ConfigGroup>
          <ConfigGroup label="Platform">
            <Tabs value={platform} onValueChange={handlePlatformChange}>
              <TabsList className="w-full">
                {snippetTabs.map((tab) => (
                  <TabsTrigger key={tab.value} value={tab.value}>
                    {tab.label}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
          </ConfigGroup>
          {mode === "existing" && (
            <SnippetField
              id="tunnel-config-mcp-url"
              label="MCP server address"
              description="Internal Streamable HTTP endpoint the agent proxies to."
              value={mcpUrlDraft}
              onChange={setMcpUrlDraft}
              placeholder={DEFAULT_MCP_URL}
            />
          )}
          <SnippetField
            id="tunnel-config-service-version"
            label="Service version"
            description="Version of the MCP service behind this tunnel."
            value={serviceVersionDraft}
            onChange={setServiceVersionDraft}
            placeholder={DEFAULT_SERVICE_VERSION}
          />
        </div>

        <div>
          <Type muted small className="mb-3">
            {activeSnippet.hint}
          </Type>
          <CodeBlock
            language={activeSnippet.language}
            slots={activeSnippet.slots}
          >
            {activeSnippet.code}
          </CodeBlock>
        </div>
      </div>
    </div>
  );
}

// Snippets for pointing the tunnel agent at an MCP server the user already
// runs; the address and version both come from the config form.
function existingServerTabs(ctx: SnippetContext): SnippetTab[] {
  const { renderedKey, slug, gateway, mcpUrl, serviceVersion } = ctx;

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

  return [
    {
      value: "kubernetes",
      label: "Kubernetes",
      language: "yaml",
      hint: "Deploy the tunnel agent in your cluster. Use your MCP server's in-cluster address, e.g. http://my-mcp.default.svc.cluster.local:3000/mcp.",
      code: kubernetes,
      slots: {
        [MCP_URL_SENTINEL]: yamlSlot(mcpUrl),
        [SERVICE_VERSION_SENTINEL]: yamlSlot(serviceVersion),
      },
    },
    {
      value: "docker",
      label: "Docker",
      language: "bash",
      hint: "Run the tunnel agent as a container. If your MCP server runs on the Docker host, use http://host.docker.internal:<port> as the address.",
      code: docker,
      slots: {
        [MCP_URL_SENTINEL]: shellSlot(mcpUrl, "TUNNEL_LOCAL_MCP_URL="),
        [SERVICE_VERSION_SENTINEL]: shellSlot(
          serviceVersion,
          "TUNNEL_SERVICE_VERSION=",
        ),
      },
    },
    {
      value: "cli",
      label: "CLI",
      language: "bash",
      hint: "Run the Gram CLI agent on the same host as your MCP server.",
      code: cli,
      slots: {
        [MCP_URL_SENTINEL]: shellSlot(mcpUrl),
        [SERVICE_VERSION_SENTINEL]: shellSlot(serviceVersion),
      },
    },
  ];
}

const HELLO_WORLD_PYTHON = `from mcp.server.fastmcp import FastMCP

mcp = FastMCP(
    "hello-world",
    host="0.0.0.0",
    port=3000,
    stateless_http=True,
    json_response=True,
)

@mcp.tool()
def hello(name: str = "world") -> str:
    """Return a friendly greeting."""
    return f"Hello, {name}!"

@mcp.resource("hello://world")
def hello_resource() -> str:
    return "Hello from a tunneled MCP server."

if __name__ == "__main__":
    mcp.run(transport="streamable-http")`;

// Snippets that stand up a sample hello-world MCP server next to the tunnel
// agent; the server's address is fixed by each deployment, so only the
// version comes from the config form.
function newServerTabs(ctx: SnippetContext): SnippetTab[] {
  const { renderedKey, slug, gateway, serviceVersion } = ctx;
  const clusterUpstream = "http://127.0.0.1:3000/mcp";
  const dockerUpstream = `http://hello-world-mcp-${slug}:3000/mcp`;
  const cliUpstream = "http://localhost:3000/mcp";

  const kubernetes = `apiVersion: v1
kind: ConfigMap
metadata:
  name: hello-world-mcp
data:
  server.py: |
${indentSnippet(HELLO_WORLD_PYTHON, 4)}
---
apiVersion: v1
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
        - name: hello-world-mcp
          image: python:3.12-slim
          command: ["/bin/sh", "-lc"]
          args:
            - |
              pip install "mcp[cli]>=1.27,<2" &&
              python /app/server.py
          ports:
            - containerPort: 3000
          volumeMounts:
            - name: hello-world-mcp
              mountPath: /app
              readOnly: true
        - name: tunnel-agent
          image: ghcr.io/speakeasy-api/gram-tunnel-agent:latest
          env:
            - name: TUNNEL_KEY
              valueFrom:
                secretKeyRef:
                  name: gram-tunnel-key
                  key: TUNNEL_KEY
            - name: TUNNEL_LOCAL_MCP_URL
              value: ${yamlQuote(clusterUpstream)}
            - name: TUNNEL_GATEWAY_URL
              value: ${yamlQuote(gateway)}
            - name: TUNNEL_SERVICE_VERSION
              value: ${yamlQuote(SERVICE_VERSION_SENTINEL)}
      volumes:
        - name: hello-world-mcp
          configMap:
            name: hello-world-mcp`;

  const docker = `mkdir -p gram-tunnel-${slug}
cd gram-tunnel-${slug}

cat > server.py <<'PY'
${HELLO_WORLD_PYTHON}
PY

cat > Dockerfile <<'DOCKERFILE'
FROM python:3.12-slim
RUN pip install "mcp[cli]>=1.27,<2"
WORKDIR /app
COPY server.py .
EXPOSE 3000
CMD ["python", "server.py"]
DOCKERFILE

docker build -t hello-world-mcp-${slug}:local .
docker network create gram-tunnel-${slug} >/dev/null 2>&1 || true
docker rm -f hello-world-mcp-${slug} gram-tunnel-${slug} >/dev/null 2>&1 || true
trap 'docker rm -f hello-world-mcp-${slug} >/dev/null 2>&1' EXIT

docker run -d --rm --name hello-world-mcp-${slug} \\
  --network gram-tunnel-${slug} \\
  hello-world-mcp-${slug}:local

docker run --rm --name gram-tunnel-${slug} \\
  --network gram-tunnel-${slug} \\
  -e TUNNEL_KEY=${shellQuote(renderedKey)} \\
  -e TUNNEL_LOCAL_MCP_URL=${shellQuote(dockerUpstream)} \\
  -e TUNNEL_GATEWAY_URL=${shellQuote(gateway)} \\
  -e TUNNEL_SERVICE_VERSION='${SERVICE_VERSION_SENTINEL}' \\
  ghcr.io/speakeasy-api/gram-tunnel-agent:latest`;

  const cli = `TUNNEL_GATEWAY_URL=${shellQuote(gateway)} \\
TUNNEL_KEY=${shellQuote(renderedKey)} \\
TUNNEL_LOCAL_MCP_URL=${shellQuote(cliUpstream)} \\
TUNNEL_SERVICE_VERSION='${SERVICE_VERSION_SENTINEL}' \\
gram tunnel run`;

  return [
    {
      value: "kubernetes",
      label: "Kubernetes",
      language: "yaml",
      hint: "Run a tiny Python hello-world MCP server and the tunnel agent in the same pod.",
      code: kubernetes,
      slots: {
        [SERVICE_VERSION_SENTINEL]: yamlSlot(serviceVersion),
      },
    },
    {
      value: "docker",
      label: "Docker",
      language: "bash",
      hint: "Build a tiny Python MCP image and run the tunnel agent on the same Docker network.",
      code: docker,
      slots: {
        [SERVICE_VERSION_SENTINEL]: shellSlot(
          serviceVersion,
          "TUNNEL_SERVICE_VERSION=",
        ),
      },
    },
    {
      value: "cli",
      label: "CLI",
      language: "bash",
      hint: "Start the hello-world MCP container from the Docker tab, then run the Gram CLI agent against it on localhost:3000.",
      code: cli,
      slots: {
        [SERVICE_VERSION_SENTINEL]: shellSlot(serviceVersion),
      },
    },
  ];
}

// Each slot's display text must reproduce the full shiki token it replaces:
// YAML emits the quoted value as one token, and bash merges a KEY='value'
// option argument into a single token (so those slots re-render the key too
// via tokenPrefix). copyText fills the bare sentinel between the quotes
// already present in the snippet text, so it is the escaped value without
// outer quotes.
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

// A labeled group in the config card, using the same eyebrow label style as
// the MCP sidebar "At a glance" card.
function ConfigGroup({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <McpSidebarInfoLabel>{label}</McpSidebarInfoLabel>
      {children}
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

function indentSnippet(value: string, spaces: number): string {
  const indent = " ".repeat(spaces);
  return value
    .split("\n")
    .map((line) => (line ? `${indent}${line}` : ""))
    .join("\n");
}

function shellEscape(value: string): string {
  return value.replace(/'/g, "'\\''");
}

function shellQuote(value: string): string {
  return `'${shellEscape(value)}'`;
}
