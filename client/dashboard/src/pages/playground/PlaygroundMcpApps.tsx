import { Type } from "@/components/ui/type";
import type { Toolset } from "@/lib/toolTypes";
import type { ToolCallMessagePartComponent } from "@assistant-ui/react";
import { useQuery } from "@tanstack/react-query";
import { LoaderCircle } from "lucide-react";
import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PropsWithChildren,
} from "react";

const MCP_PROTOCOL_VERSION = "2025-06-18";
const MCP_APP_MIME_TYPE = "text/html;profile=mcp-app";
const DEFAULT_APP_HEIGHT = 360;
const MAX_APP_HEIGHT = 560;

type JsonRpcId = number | string;

type JsonRpcRequest = {
  id: JsonRpcId;
  jsonrpc: "2.0";
  method: string;
  params?: unknown;
};

type JsonRpcNotification = {
  jsonrpc: "2.0";
  method: string;
  params?: unknown;
};

type JsonRpcResponse = {
  id: JsonRpcId;
  jsonrpc: "2.0";
  result?: unknown;
  error?: {
    code: number;
    message: string;
  };
};

type McpAppToolBinding = {
  description: string;
  inputSchema?: unknown;
  name: string;
  resourceUri: string;
};

type McpAppResourceListing = {
  meta?: Record<string, unknown>;
  mimeType?: string;
  uri: string;
};

type McpUiMeta = {
  csp?: {
    baseUriDomains?: string[];
    connectDomains?: string[];
    frameDomains?: string[];
    resourceDomains?: string[];
  };
  permissions?: {
    camera?: Record<string, never>;
    clipboardWrite?: Record<string, never>;
    geolocation?: Record<string, never>;
    microphone?: Record<string, never>;
  };
  prefersBorder?: boolean;
};

type McpAppResourceContent = {
  html: string;
  meta?: Record<string, unknown>;
  mimeType?: string;
  uri: string;
};

type PlaygroundMcpAppsValue = {
  headers: Record<string, string> | null;
  mcpUrl: string;
  resources: Map<string, McpAppResourceListing>;
  theme: "dark" | "light";
  toolBindings: Map<string, McpAppToolBinding>;
};

const PlaygroundMcpAppsContext = createContext<PlaygroundMcpAppsValue | null>(
  null,
);

export function PlaygroundMcpAppsProvider({
  children,
  headers,
  mcpUrl,
  theme,
  toolset,
}: PropsWithChildren<{
  headers: Record<string, string> | null;
  mcpUrl: string;
  theme: "dark" | "light";
  toolset: Toolset | undefined;
}>) {
  const value = useMemo<PlaygroundMcpAppsValue>(() => {
    const toolBindings = new Map<string, McpAppToolBinding>();
    for (const tool of toolset?.rawTools ?? []) {
      const metadata = getToolMetadata(tool);
      const resourceUri = getResourceUri(metadata);
      const toolInfo = getToolInfo(tool);
      if (!resourceUri || !toolInfo) {
        continue;
      }

      toolBindings.set(toolInfo.name, {
        description: toolInfo.description,
        inputSchema: toolInfo.inputSchema,
        name: toolInfo.name,
        resourceUri,
      });
    }

    const resources = new Map<string, McpAppResourceListing>();
    for (const resource of toolset?.resources ?? []) {
      resources.set(resource.uri, {
        meta: resource.meta,
        mimeType: resource.mimeType,
        uri: resource.uri,
      });
    }

    return {
      headers,
      mcpUrl,
      resources,
      theme,
      toolBindings,
    };
  }, [headers, mcpUrl, theme, toolset]);

  return (
    <PlaygroundMcpAppsContext.Provider value={value}>
      {children}
    </PlaygroundMcpAppsContext.Provider>
  );
}

export const PlaygroundMcpToolFallback: ToolCallMessagePartComponent = ({
  args,
  result,
  status,
  toolCallId,
  toolName,
}) => {
  const ctx = useContext(PlaygroundMcpAppsContext);
  const binding = ctx?.toolBindings.get(toolName);
  const statusLabel =
    status.type === "complete"
      ? "Complete"
      : status.type === "incomplete"
        ? "Failed"
        : "Running";

  return (
    <div className="flex w-full flex-col border-b border-border last:border-b-0">
      <div className="px-4 py-4">
        <div className="flex items-start justify-between gap-3">
          <div className="space-y-1">
            <Type variant="small" className="font-medium">
              {toolName}
            </Type>
            <Type muted className="text-xs">
              {binding?.description ?? "Tool execution"}
            </Type>
          </div>
          <span className="rounded-full border border-border bg-background px-2 py-1 text-[11px] font-medium uppercase tracking-[0.08em] text-muted-foreground">
            {statusLabel}
          </span>
        </div>

        <details className="mt-3 overflow-auto rounded-xl border border-border bg-background">
          <summary className="cursor-pointer px-3 py-2 text-xs font-medium text-muted-foreground">
            Request / Result
          </summary>
          <div className="grid gap-3 border-t border-border px-3 py-3">
            <JsonPanel label="Request" value={args} />
            <JsonPanel label="Result" value={result} />
          </div>
        </details>
      </div>

      {binding?.resourceUri && status.type === "complete" && (
        <PlaygroundMcpAppRenderer
          key={`${toolCallId}:${binding.resourceUri}`}
          args={args}
          result={result}
          tool={binding}
        />
      )}
    </div>
  );
};

function PlaygroundMcpAppRenderer({
  args,
  result,
  tool,
}: {
  args: unknown;
  result: unknown;
  tool: McpAppToolBinding;
}) {
  const ctx = useContext(PlaygroundMcpAppsContext);
  const mcpUrl = ctx?.mcpUrl ?? "";
  const headers = ctx?.headers ?? null;
  const listing = ctx?.resources.get(tool.resourceUri);
  const resourceQuery = useQuery({
    queryKey: [
      "playground-mcp-app-resource",
      mcpUrl,
      tool.resourceUri,
      JSON.stringify(Object.entries(headers ?? {}).sort()),
    ],
    queryFn: async () => {
      if (!headers) {
        throw new Error("Missing MCP headers");
      }
      const readResult = await requestMcpJsonRpc<{
        contents?: Array<{
          _meta?: Record<string, unknown>;
          blob?: string;
          mimeType?: string;
          text?: string;
          uri: string;
        }>;
      }>(mcpUrl, headers, {
        method: "resources/read",
        params: { uri: tool.resourceUri },
      });

      const content = readResult.contents?.[0];
      if (!content) {
        throw new Error(`Resource ${tool.resourceUri} returned no content`);
      }

      return {
        html:
          typeof content.text === "string"
            ? content.text
            : decodeHtmlBlob(content.blob),
        meta: content._meta,
        mimeType: content.mimeType,
        uri: content.uri,
      } satisfies McpAppResourceContent;
    },
    enabled: !!headers,
    staleTime: Infinity,
  });

  if (!ctx) {
    return null;
  }

  if (resourceQuery.isLoading) {
    return (
      <div className="flex items-center gap-2 border-t border-border px-4 py-4 text-sm text-muted-foreground">
        <LoaderCircle className="size-4 animate-spin" />
        Loading MCP app resource…
      </div>
    );
  }

  if (resourceQuery.isError) {
    return (
      <div className="border-t border-border px-4 py-4">
        <Type variant="small" className="font-medium">
          MCP app failed to load
        </Type>
        <Type muted className="mt-1 text-xs">
          {String(
            resourceQuery.error instanceof Error
              ? resourceQuery.error.message
              : resourceQuery.error,
          )}
        </Type>
      </div>
    );
  }

  const resource = resourceQuery.data;
  const mimeType = resource?.mimeType ?? listing?.mimeType;
  if (!resource || mimeType !== MCP_APP_MIME_TYPE) {
    return (
      <div className="border-t border-border px-4 py-4">
        <Type variant="small" className="font-medium">
          Unsupported MCP app resource
        </Type>
        <Type muted className="mt-1 text-xs">
          Playground currently expects `{MCP_APP_MIME_TYPE}`.
        </Type>
      </div>
    );
  }

  return (
    <McpAppFrame
      args={args}
      listing={listing}
      resource={resource}
      result={result}
      theme={ctx.theme}
      tool={tool}
      transport={ctx}
    />
  );
}

function McpAppFrame({
  args,
  listing,
  resource,
  result,
  theme,
  tool,
  transport,
}: {
  args: unknown;
  listing: McpAppResourceListing | undefined;
  resource: McpAppResourceContent;
  result: unknown;
  theme: "dark" | "light";
  tool: McpAppToolBinding;
  transport: PlaygroundMcpAppsValue;
}) {
  const iframeRef = useRef<HTMLIFrameElement | null>(null);
  const [isInitialized, setIsInitialized] = useState(false);
  const [height, setHeight] = useState(DEFAULT_APP_HEIGHT);
  const uiMeta = getUiMeta(resource.meta) ?? getUiMeta(listing?.meta);

  useEffect(() => {
    setIsInitialized(false);
  }, [resource.uri]);

  useEffect(() => {
    if (!iframeRef.current) {
      return;
    }

    const handleMessage = async (event: MessageEvent) => {
      if (event.source !== iframeRef.current?.contentWindow) {
        return;
      }

      const message = event.data as
        | JsonRpcNotification
        | JsonRpcRequest
        | undefined;
      if (!message || message.jsonrpc !== "2.0") {
        return;
      }

      if ("id" in message) {
        const response = await handleIframeRequest({
          message,
          tool,
          transport,
          uiMeta,
          theme,
        });
        iframeRef.current?.contentWindow?.postMessage(response, "*");
        return;
      }

      switch (message.method) {
        case "ui/notifications/initialized":
          setIsInitialized(true);
          break;
        case "ui/notifications/size-changed": {
          const requestedHeight = Number(
            (message.params as { height?: number } | undefined)?.height,
          );
          if (Number.isFinite(requestedHeight) && requestedHeight > 0) {
            setHeight(Math.min(Math.max(requestedHeight, 220), MAX_APP_HEIGHT));
          }
          break;
        }
        case "notifications/message":
          console.info("playground mcp app:", message.params);
          break;
        default:
          break;
      }
    };

    window.addEventListener("message", handleMessage);
    return () => {
      window.removeEventListener("message", handleMessage);
    };
  }, [theme, tool, transport, uiMeta]);

  useEffect(() => {
    if (!isInitialized) {
      return;
    }

    iframeRef.current?.contentWindow?.postMessage(
      {
        jsonrpc: "2.0",
        method: "ui/notifications/tool-input",
        params: { arguments: args ?? {} },
      } satisfies JsonRpcNotification,
      "*",
    );
    iframeRef.current?.contentWindow?.postMessage(
      {
        jsonrpc: "2.0",
        method: "ui/notifications/tool-result",
        params: result ?? {},
      } satisfies JsonRpcNotification,
      "*",
    );
  }, [args, isInitialized, result]);

  useEffect(() => {
    if (!isInitialized) {
      return;
    }

    iframeRef.current?.contentWindow?.postMessage(
      {
        jsonrpc: "2.0",
        method: "ui/notifications/host-context-changed",
        params: {
          displayMode: "inline",
          theme,
        },
      } satisfies JsonRpcNotification,
      "*",
    );
  }, [isInitialized, theme]);

  return (
    <div className="border-t border-border px-4 pb-4">
      <div className="overflow-hidden rounded-2xl border border-border bg-background shadow-sm">
        <iframe
          ref={iframeRef}
          allow={buildAllowAttribute(uiMeta)}
          className="block w-full bg-transparent"
          sandbox="allow-forms allow-modals allow-popups allow-popups-to-escape-sandbox allow-scripts"
          srcDoc={withCsp(resource.html, uiMeta?.csp)}
          style={{ height }}
          title={`${tool.name} app`}
        />
      </div>
    </div>
  );
}

async function handleIframeRequest(init: {
  message: JsonRpcRequest;
  tool: McpAppToolBinding;
  transport: PlaygroundMcpAppsValue;
  uiMeta: McpUiMeta | undefined;
  theme: "dark" | "light";
}): Promise<JsonRpcResponse> {
  const { message, tool, transport, uiMeta, theme } = init;

  try {
    switch (message.method) {
      case "ping":
        return ok(message.id, {});
      case "ui/initialize":
        return ok(message.id, {
          hostCapabilities: {
            logging: {},
            openLinks: {},
            serverResources: {},
            serverTools: {},
          },
          hostContext: {
            availableDisplayModes: ["inline"],
            containerDimensions: {
              maxHeight: MAX_APP_HEIGHT,
            },
            displayMode: "inline",
            locale: navigator.language,
            platform: "web",
            theme,
            timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone,
            toolInfo: {
              tool: {
                _meta: {
                  ui: {
                    resourceUri: tool.resourceUri,
                  },
                },
                description: tool.description,
                inputSchema: tool.inputSchema ?? {
                  type: "object",
                  properties: {},
                },
                name: tool.name,
              },
            },
            userAgent: "gram-playground",
          },
          hostInfo: {
            name: "gram-playground",
            version: "0.0.1",
          },
          protocolVersion: MCP_PROTOCOL_VERSION,
          serverInfo: {
            name: "gram-playground",
            version: "0.0.1",
          },
          ...(uiMeta
            ? {
                sandbox: {
                  csp: uiMeta.csp,
                  permissions: uiMeta.permissions,
                },
              }
            : {}),
        });
      case "ui/open-link": {
        const url = (message.params as { url?: string } | undefined)?.url;
        if (!url) {
          return err(message.id, -32602, "Missing URL");
        }
        window.open(url, "_blank", "noopener,noreferrer");
        return ok(message.id, {});
      }
      case "resources/read":
      case "tools/call":
        if (!transport.headers) {
          return err(message.id, -32000, "Missing MCP session headers");
        }
        return ok(
          message.id,
          await requestMcpJsonRpc(transport.mcpUrl, transport.headers, message),
        );
      default:
        return err(
          message.id,
          -32601,
          `Method not supported in playground host: ${message.method}`,
        );
    }
  } catch (error) {
    return err(
      message.id,
      -32000,
      error instanceof Error ? error.message : "Unknown playground host error",
    );
  }
}

async function requestMcpJsonRpc<T>(
  url: string,
  headers: Record<string, string>,
  request: Omit<JsonRpcRequest, "id" | "jsonrpc"> | JsonRpcRequest,
): Promise<T> {
  const body: JsonRpcRequest =
    "id" in request
      ? request
      : {
          id: crypto.randomUUID(),
          jsonrpc: "2.0",
          method: request.method,
          params: request.params,
        };

  const response = await fetch(url, {
    body: JSON.stringify(body),
    headers: {
      Accept: "application/json",
      "Content-Type": "application/json",
      ...headers,
    },
    method: "POST",
  });

  if (!response.ok) {
    throw new Error(`MCP request failed with status ${response.status}`);
  }

  const payload = (await response.json()) as {
    error?: { message?: string };
    result?: T;
  };
  if (payload.error) {
    throw new Error(payload.error.message ?? "MCP request failed");
  }

  return payload.result as T;
}

function ok(id: JsonRpcId, result: unknown): JsonRpcResponse {
  return {
    id,
    jsonrpc: "2.0",
    result,
  };
}

function err(id: JsonRpcId, code: number, message: string): JsonRpcResponse {
  return {
    error: {
      code,
      message,
    },
    id,
    jsonrpc: "2.0",
  };
}

function getToolInfo(tool: Toolset["rawTools"][number]) {
  if (tool.functionToolDefinition) {
    return {
      description: tool.functionToolDefinition.description,
      inputSchema: safeJsonParse(tool.functionToolDefinition.schema),
      name: tool.functionToolDefinition.name,
    };
  }
  if (tool.httpToolDefinition) {
    return {
      description: tool.httpToolDefinition.description,
      inputSchema: safeJsonParse(tool.httpToolDefinition.schema),
      name: tool.httpToolDefinition.name,
    };
  }
  if (tool.externalMcpToolDefinition) {
    return {
      description: tool.externalMcpToolDefinition.description,
      inputSchema: safeJsonParse(tool.externalMcpToolDefinition.schema),
      name: tool.externalMcpToolDefinition.name,
    };
  }
  if (tool.promptTemplate) {
    return {
      description: tool.promptTemplate.description,
      inputSchema: safeJsonParse(tool.promptTemplate.schema),
      name: tool.promptTemplate.name,
    };
  }
  return undefined;
}

function getToolMetadata(tool: Toolset["rawTools"][number]) {
  return tool.functionToolDefinition?.meta;
}

function getResourceUri(meta: Record<string, unknown> | undefined) {
  const nested = meta?.ui;
  if (
    nested &&
    typeof nested === "object" &&
    typeof (nested as { resourceUri?: unknown }).resourceUri === "string"
  ) {
    return (nested as { resourceUri: string }).resourceUri;
  }

  if (typeof meta?.["ui/resourceUri"] === "string") {
    return meta["ui/resourceUri"] as string;
  }

  return undefined;
}

function getUiMeta(meta: Record<string, unknown> | undefined) {
  const value = meta?.ui;
  if (!value || typeof value !== "object") {
    return undefined;
  }
  return value as McpUiMeta;
}

function safeJsonParse(input: string | undefined) {
  if (!input) {
    return undefined;
  }
  try {
    return JSON.parse(input);
  } catch {
    return undefined;
  }
}

function decodeHtmlBlob(blob: string | undefined) {
  if (!blob) {
    return "";
  }

  const binary = atob(blob);
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

function buildAllowAttribute(uiMeta: McpUiMeta | undefined) {
  const permissions = uiMeta?.permissions;
  const values: string[] = [];
  if (permissions?.camera) {
    values.push("camera");
  }
  if (permissions?.microphone) {
    values.push("microphone");
  }
  if (permissions?.geolocation) {
    values.push("geolocation");
  }
  if (permissions?.clipboardWrite) {
    values.push("clipboard-write");
  }
  return values.join("; ");
}

function withCsp(html: string, csp: McpUiMeta["csp"] | undefined): string {
  const content = buildCsp(csp);
  const metaTag = `<meta http-equiv="Content-Security-Policy" content="${escapeAttribute(content)}">`;

  if (html.includes("<head>")) {
    return html.replace("<head>", `<head>${metaTag}`);
  }

  if (html.includes("<head ")) {
    return html.replace(/<head([^>]*)>/, `<head$1>${metaTag}`);
  }

  return `${metaTag}${html}`;
}

function buildCsp(csp: McpUiMeta["csp"] | undefined) {
  const connectSrc = buildDirective(csp?.connectDomains, "'none'");
  const resourceSrc = buildDirective(csp?.resourceDomains, "'self' data:");
  const frameSrc = buildDirective(csp?.frameDomains, "'none'");
  const baseUri = buildDirective(csp?.baseUriDomains, "'self'");

  return [
    "default-src 'none'",
    `script-src ${resourceSrc} 'unsafe-inline'`,
    `style-src ${resourceSrc} 'unsafe-inline'`,
    `img-src ${resourceSrc}`,
    `font-src ${resourceSrc}`,
    `media-src ${resourceSrc}`,
    `connect-src ${connectSrc}`,
    `frame-src ${frameSrc}`,
    `base-uri ${baseUri}`,
    "object-src 'none'",
  ].join("; ");
}

function buildDirective(domains: string[] | undefined, fallback: string) {
  if (!domains || domains.length === 0) {
    return fallback;
  }
  return domains.join(" ");
}

function escapeAttribute(input: string) {
  return input.replace(/&/g, "&amp;").replace(/"/g, "&quot;");
}

function JsonPanel({ label, value }: { label: string; value: unknown }) {
  return (
    <div className="space-y-1">
      <Type
        muted
        className="text-[11px] font-medium uppercase tracking-[0.08em]"
      >
        {label}
      </Type>
      <pre className="overflow-x-auto rounded-lg border border-border bg-muted/40 p-3 font-mono text-xs leading-5 text-foreground">
        {formatJson(value)}
      </pre>
    </div>
  );
}

function formatJson(value: unknown) {
  if (value === undefined) {
    return "undefined";
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}
