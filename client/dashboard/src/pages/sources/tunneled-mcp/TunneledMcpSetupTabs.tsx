import { shellQuote } from "@/lib/shell";
import { CodeBlock, type CodeBlockSlot } from "@/components/code";
import { DetailSidebarInfoLabel } from "@/components/detail/detail-sidebar-nav";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/Tabs";
import { Text } from "@/components/ui/Text";
import { cn, tunnelGatewayURL } from "@/lib/utils";
import { Badge } from "@/components/ui/Badge";
import { useOrganization } from "@/contexts/Auth";
import { useEffect, useRef, useState } from "react";

const DEFAULT_MCP_URL = "https://placeholder.net/mcp";
const DEFAULT_MCP_COMMAND = "npx -y @modelcontextprotocol/server-everything";
// Per-user credentials need a pinned server that reads its token from
// SPEAKEASY_ACCESS_TOKEN_FILE; exec keeps it in the agent's process group.
const DEFAULT_CREDENTIALS_MCP_COMMAND = "exec /opt/mcp/bin/your-mcp-server";
const PRODUCTION_ASSERTION_ISSUER = "https://tunnel.speakeasy.com";
const DEFAULT_SERVICE_VERSION = "1.0.0";
const TUNNEL_AGENT_IMAGE = `ghcr.io/speakeasy-api/gram-tunnel-agent:${__GRAM_TUNNEL_AGENT_VERSION__}`;

// Sentinels embedded in the snippet text; CodeBlock swaps the shiki token
// containing each one for a live-updating <FlashOnChange> node, so the code
// string itself is stable across keystrokes and never re-tokenizes.
const MCP_URL_SENTINEL = "__SLOT_mcpUrl__";
const MCP_COMMAND_SENTINEL = "__SLOT_mcpCommand__";
const SERVICE_VERSION_SENTINEL = "__SLOT_serviceVersion__";
const ISSUER_SENTINEL = "__SLOT_assertionIssuer__";

type SetupMode = "existing" | "new";

type Transport = "http" | "stdio";

type CredentialMode = "shared" | "user";

type CredentialSettings = {
  issuer: string;
  audience: string;
  organizationId: string;
};

type Platform = "kubernetes" | "docker";

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
  mcpCommand: string;
  serviceVersion: string;
  // Set when the stdio server acts upstream as each Speakeasy user.
  credentials?: CredentialSettings;
};

export function TunneledMcpSetupTabs({
  tunnelKey,
  keyPrefix,
  serverName,
  tunneledMcpServerId,
  resourceIdentifier,
}: {
  tunnelKey?: string;
  keyPrefix?: string;
  serverName?: string;
  // The tunneled source, when known, fills in the assertion audience.
  tunneledMcpServerId?: string;
  resourceIdentifier?: string;
}): JSX.Element {
  const organization = useOrganization();
  const [mode, setMode] = useState<SetupMode>("existing");
  const [platform, setPlatform] = useState<Platform>("kubernetes");
  const [transport, setTransport] = useState<Transport>("http");
  const [mcpUrlDraft, setMcpUrlDraft] = useState("");
  const [mcpCommandDraft, setMcpCommandDraft] = useState("");
  const [serviceVersionDraft, setServiceVersionDraft] = useState("");
  const [credentialMode, setCredentialMode] =
    useState<CredentialMode>("shared");
  const [issuerDraft, setIssuerDraft] = useState("");

  const gateway = tunnelGatewayURL();
  const perUserCredentials =
    mode === "existing" && transport === "stdio" && credentialMode === "user";
  const defaultIssuer = assertionIssuerFor(gateway);
  const defaultCommand = perUserCredentials
    ? DEFAULT_CREDENTIALS_MCP_COMMAND
    : DEFAULT_MCP_COMMAND;

  const ctx: SnippetContext = {
    renderedKey: tunnelKey ?? "<YOUR_TUNNEL_KEY>",
    slug: slugForSnippet(serverName),
    gateway,
    mcpUrl: mcpUrlDraft.trim() || DEFAULT_MCP_URL,
    mcpCommand: mcpCommandDraft.trim() || defaultCommand,
    serviceVersion: serviceVersionDraft.trim() || DEFAULT_SERVICE_VERSION,
    credentials: perUserCredentials
      ? {
          issuer: issuerDraft.trim() || defaultIssuer,
          audience: assertionAudienceFor(
            tunneledMcpServerId,
            resourceIdentifier,
          ),
          organizationId: organization.id,
        }
      : undefined,
  };

  const snippetTabs = snippetTabsFor(mode, transport, ctx);
  const activeSnippet =
    snippetTabs.find((tab) => tab.value === platform) ?? snippetTabs[0]!;

  const handleModeChange = (value: string) => {
    if (value === "existing" || value === "new") setMode(value);
  };

  const handleTransportChange = (value: string) => {
    if (value === "http" || value === "stdio") setTransport(value);
  };

  const handleCredentialModeChange = (value: string) => {
    if (value === "shared" || value === "user") setCredentialMode(value);
  };

  const handlePlatformChange = (value: string) => {
    if (value === "kubernetes" || value === "docker") {
      setPlatform(value);
    }
  };

  return (
    <div className="border p-6">
      <div className="mb-5 flex flex-wrap items-start justify-between gap-3">
        <div>
          <Text variant="subheading">Connect your MCP server</Text>
          <Text muted small className="mt-1">
            Start a tunnel agent next to the MCP server you already run.
          </Text>
        </div>
        {keyPrefix && (
          <Badge variant="neutral">
            <Badge.Text>{keyPrefix}</Badge.Text>
          </Badge>
        )}
      </div>

      <div className="grid grid-cols-1 items-start gap-6 lg:grid-cols-[300px_minmax(0,1fr)]">
        <div className="bg-card border-border flex flex-col gap-3 border px-4 py-3 shadow-md lg:sticky lg:top-[calc(var(--page-sticky-top,0px)+1.5rem)] dark:bg-neutral-950">
          <Text className="font-semibold">Tunnel config</Text>
          <ConfigGroup label="Tunnel endpoint">
            <Tabs value={mode} onValueChange={handleModeChange}>
              <TabsList className="w-full">
                <TabsTrigger value="existing">Existing server</TabsTrigger>
                <TabsTrigger value="new">New server</TabsTrigger>
              </TabsList>
            </Tabs>
            <Text muted small>
              {mode === "existing"
                ? "Point the tunnel agent at an MCP server you already run. Values are templated into the setup snippet."
                : "Deploy a sample hello-world MCP server together with the tunnel agent to try the tunnel end to end."}
            </Text>
          </ConfigGroup>
          {mode === "existing" && (
            <ConfigGroup label="Transport">
              <Tabs value={transport} onValueChange={handleTransportChange}>
                <TabsList className="w-full">
                  <TabsTrigger value="http">HTTP</TabsTrigger>
                  <TabsTrigger value="stdio">Stdio</TabsTrigger>
                </TabsList>
              </Tabs>
              <Text muted small>
                {TRANSPORT_DESCRIPTIONS[transport]}
              </Text>
            </ConfigGroup>
          )}
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
          {mode === "existing" && transport === "http" && (
            <SnippetField
              id="tunnel-config-mcp-url"
              label="MCP server address"
              description="Internal Streamable HTTP endpoint the agent proxies to."
              value={mcpUrlDraft}
              onChange={setMcpUrlDraft}
              placeholder={DEFAULT_MCP_URL}
            />
          )}
          {mode === "existing" && transport === "stdio" && (
            <SnippetField
              id="tunnel-config-mcp-command"
              label="Server command"
              description="Shell command that starts the stdio MCP server. The agent runs it once per MCP session."
              value={mcpCommandDraft}
              onChange={setMcpCommandDraft}
              placeholder={defaultCommand}
            />
          )}
          {mode === "existing" && transport === "stdio" && (
            <ConfigGroup label="Upstream credentials">
              <Tabs
                value={credentialMode}
                onValueChange={handleCredentialModeChange}
              >
                <TabsList className="w-full">
                  <TabsTrigger value="shared">Shared</TabsTrigger>
                  <TabsTrigger value="user">Per user</TabsTrigger>
                </TabsList>
              </Tabs>
              <Text muted small>
                {CREDENTIAL_MODE_DESCRIPTIONS[credentialMode]}
              </Text>
            </ConfigGroup>
          )}
          {perUserCredentials && (
            <SnippetField
              id="tunnel-config-assertion-issuer"
              label="Assertion issuer"
              description="Issuer of Speakeasy's signed caller assertions for this deployment. Production uses https://tunnel.speakeasy.com; verify it for other deployments."
              value={issuerDraft}
              onChange={setIssuerDraft}
              placeholder={defaultIssuer}
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
          <Text muted small className="mb-3">
            {activeSnippet.hint}
          </Text>
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

const CREDENTIAL_MODE_DESCRIPTIONS: Record<CredentialMode, string> = {
  shared:
    "Every server process uses the credentials in the agent's environment.",
  user: "Each Speakeasy user's own upstream token is written to their server process's SPEAKEASY_ACCESS_TOKEN_FILE. Every MCP server on this tunnel must be private, and callers without a linked account are refused. The server must be a pinned, trusted build that reads that file.",
};

// The production issuer on production hosts; elsewhere the gateway's origin,
// which the editable field lets people correct.
function assertionIssuerFor(gateway: string): string {
  const url = new URL(gateway);
  if (url.hostname === "tunnel.speakeasy.com") {
    return PRODUCTION_ASSERTION_ISSUER;
  }
  return `${url.protocol === "ws:" ? "http" : "https"}://${url.host}`;
}

function assertionAudienceFor(
  tunneledMcpServerId: string | undefined,
  resourceIdentifier: string | undefined,
): string {
  if (resourceIdentifier) return resourceIdentifier;
  return `tunneled-mcp-server:${tunneledMcpServerId ?? "<TUNNELED_MCP_SERVER_ID>"}`;
}

const TRANSPORT_DESCRIPTIONS: Record<Transport, string> = {
  http: "The MCP server listens on a Streamable HTTP endpoint.",
  stdio:
    "The MCP server speaks over stdin and stdout. The agent starts one server process per MCP session.",
};

function snippetTabsFor(
  mode: SetupMode,
  transport: Transport,
  ctx: SnippetContext,
): SnippetTab[] {
  if (mode === "new") return newServerTabs(ctx);
  switch (transport) {
    case "http":
      return existingServerTabs(ctx);
    case "stdio":
      return stdioServerTabs(ctx);
  }
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
          image: ${TUNNEL_AGENT_IMAGE}
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
  ${TUNNEL_AGENT_IMAGE}`;

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
  ];
}

// The published agent image has no language runtime for the command to use.
function stdioServerTabs(ctx: SnippetContext): SnippetTab[] {
  const {
    renderedKey,
    slug,
    gateway,
    mcpCommand,
    serviceVersion,
    credentials,
  } = ctx;
  const localImage = `gram-tunnel-${slug}:local`;

  const dockerfile = credentials
    ? `cat > Dockerfile <<'DOCKERFILE'
# Any Linux base image with your server's runtime works. Install a pinned
# build of a server that reads its token from SPEAKEASY_ACCESS_TOKEN_FILE.
FROM node:22-alpine
# tini as PID 1 stops every server process when the container stops.
RUN apk add --no-cache tini
COPY --from=${TUNNEL_AGENT_IMAGE} /usr/local/bin/tunnel-agent /usr/local/bin/tunnel-agent
USER node
ENV TUNNEL_LOCAL_MCP_COMMAND="${MCP_COMMAND_SENTINEL}"
ENV TUNNEL_STDIO_CREDENTIALS=user
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/tunnel-agent"]
DOCKERFILE`
    : `cat > Dockerfile <<'DOCKERFILE'
# Any Linux base image with your command's runtime works. Keep USER pointed
# at a non-root user in that image with a writable home directory.
FROM node:22-alpine
COPY --from=${TUNNEL_AGENT_IMAGE} /usr/local/bin/tunnel-agent /usr/local/bin/tunnel-agent
USER node
ENV TUNNEL_LOCAL_MCP_COMMAND="${MCP_COMMAND_SENTINEL}"
ENTRYPOINT ["/usr/local/bin/tunnel-agent"]
DOCKERFILE`;
  const dockerCredentialFlags = credentials
    ? `  -e TUNNEL_IDENTITY_ISSUER='${ISSUER_SENTINEL}' \\
  -e TUNNEL_IDENTITY_AUDIENCE=${shellQuote(credentials.audience)} \\
  -e TUNNEL_IDENTITY_ORGANIZATION_ID=${shellQuote(credentials.organizationId)} \\
`
    : "";
  const kubernetesCredentialEnv = credentials
    ? `
            - name: TUNNEL_STDIO_CREDENTIALS
              value: "user"
            - name: TUNNEL_IDENTITY_ISSUER
              value: ${yamlQuote(ISSUER_SENTINEL)}
            - name: TUNNEL_IDENTITY_AUDIENCE
              value: ${yamlQuote(credentials.audience)}
            - name: TUNNEL_IDENTITY_ORGANIZATION_ID
              value: ${yamlQuote(credentials.organizationId)}
          # Token files live only in memory.
          volumeMounts:
            - name: credentials
              mountPath: /dev/shm
          securityContext:
            runAsNonRoot: true
            allowPrivilegeEscalation: false
      volumes:
        - name: credentials
          emptyDir:
            medium: Memory`
    : "";

  const docker = `mkdir -p gram-tunnel-${slug}
cd gram-tunnel-${slug}

${dockerfile}

docker build -t ${localImage} .
docker run --rm --name gram-tunnel-${slug} \\
  -e TUNNEL_KEY=${shellQuote(renderedKey)} \\
  -e TUNNEL_GATEWAY_URL=${shellQuote(gateway)} \\
  -e TUNNEL_SERVICE_VERSION='${SERVICE_VERSION_SENTINEL}' \\
${dockerCredentialFlags}  ${localImage}`;

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
  # MCP sessions live in one agent's server processes; keep one replica.
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
          # Built from the Dockerfile on the Docker tab and pushed to your registry.
          image: registry.example.com/gram-tunnel-${slug}:${__GRAM_TUNNEL_AGENT_VERSION__}
          env:
            - name: TUNNEL_KEY
              valueFrom:
                secretKeyRef:
                  name: gram-tunnel-key
                  key: TUNNEL_KEY
            - name: TUNNEL_GATEWAY_URL
              value: ${yamlQuote(gateway)}
            - name: TUNNEL_SERVICE_VERSION
              value: ${yamlQuote(SERVICE_VERSION_SENTINEL)}${kubernetesCredentialEnv}`;

  const issuerSlots = (
    slot: (value: string) => CodeBlockSlot,
  ): Record<string, CodeBlockSlot> =>
    credentials ? { [ISSUER_SENTINEL]: slot(credentials.issuer) } : {};

  return [
    {
      value: "kubernetes",
      label: "Kubernetes",
      language: "yaml",
      hint: "Build the image from the Docker tab, push it to a registry your cluster can pull from, and replace the image below.",
      code: kubernetes,
      slots: {
        [SERVICE_VERSION_SENTINEL]: yamlSlot(serviceVersion),
        ...issuerSlots(yamlSlot),
      },
    },
    {
      value: "docker",
      label: "Docker",
      language: "bash",
      hint: "Build an image that adds the tunnel agent and your server command to a base image with your server's runtime, then run it.",
      code: docker,
      slots: {
        [MCP_COMMAND_SENTINEL]: dockerfileEnvSlot(
          mcpCommand,
          "ENV TUNNEL_LOCAL_MCP_COMMAND=",
        ),
        [SERVICE_VERSION_SENTINEL]: shellSlot(
          serviceVersion,
          "TUNNEL_SERVICE_VERSION=",
        ),
        ...issuerSlots((value) => shellSlot(value, "TUNNEL_IDENTITY_ISSUER=")),
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
          image: ${TUNNEL_AGENT_IMAGE}
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
  ${TUNNEL_AGENT_IMAGE}`;

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

function dockerfileEnvSlot(value: string, tokenPrefix: string): CodeBlockSlot {
  return {
    node: (
      <FlashOnChange text={`${tokenPrefix}"${dockerfileEnvEscape(value)}"`} />
    ),
    copyText: dockerfileEnvEscape(value),
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
        "transition-colors",
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
      <DetailSidebarInfoLabel>{label}</DetailSidebarInfoLabel>
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
      <Text muted small>
        {description}
      </Text>
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

// A double-quoted Dockerfile ENV value expands $VARS and takes backslash escapes.
function dockerfileEnvEscape(value: string): string {
  return value.replace(/[\\"$]/g, "\\$&");
}

function shellEscape(value: string): string {
  return value.replace(/'/g, "'\\''");
}
