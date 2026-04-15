import { useEffect, useRef, useState } from "react";
import { Mermaid } from "./Mermaid";

type Payload = {
  diagram: string;
  title?: string;
};

const DEMO: Payload = {
  title: "Demo — dev preview",
  diagram: `sequenceDiagram
    participant User
    participant Gram
    participant Widget
    User->>Gram: render_diagram(source)
    Gram->>Widget: tool-result
    Widget-->>User: rendered SVG`,
};

function isPayload(data: unknown): data is Payload {
  return (
    !!data &&
    typeof data === "object" &&
    "diagram" in data &&
    typeof (data as { diagram: unknown }).diagram === "string"
  );
}

type JsonRpcMessage = {
  jsonrpc: "2.0";
  id?: number | string;
  method?: string;
  params?: unknown;
  result?: unknown;
};

function post(msg: JsonRpcMessage) {
  window.parent.postMessage(msg, "*");
}

function extractResult(params: unknown): Payload | null {
  if (!params || typeof params !== "object") return null;
  const p = params as Record<string, unknown>;

  if (isPayload(p.structuredContent)) return p.structuredContent;
  if (isPayload(p)) return p;

  // Gram serializes tool JSON responses as CallToolResult.content[0].text
  // (see server/internal/mcp/rpc_tools_call.go:formatResult). Parse it.
  const content = p.content;
  if (Array.isArray(content)) {
    for (const chunk of content) {
      if (
        chunk &&
        typeof chunk === "object" &&
        (chunk as { type?: unknown }).type === "text" &&
        typeof (chunk as { text?: unknown }).text === "string"
      ) {
        try {
          const parsed = JSON.parse((chunk as { text: string }).text);
          if (isPayload(parsed)) return parsed;
        } catch {
          // fall through
        }
      }
    }
  }

  return null;
}

export function App() {
  const [payload, setPayload] = useState<Payload | null>(null);
  const [height, setHeight] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);

  // MCP UI JSON-RPC bridge.
  useEffect(() => {
    const inIframe = window.parent !== window;
    if (!inIframe) {
      setPayload(DEMO);
      return;
    }

    const onMessage = (event: MessageEvent) => {
      const data = event.data as JsonRpcMessage | undefined;
      if (!data || data.jsonrpc !== "2.0") return;

      // Requests need a response.
      if (typeof data.id !== "undefined" && data.method) {
        if (data.method === "ping") {
          post({ jsonrpc: "2.0", id: data.id, result: {} });
          return;
        }
        if (data.method === "ui/initialize") {
          post({
            jsonrpc: "2.0",
            id: data.id,
            result: {
              appInfo: { name: "mermaid", version: "0.0.0" },
              capabilities: {},
            },
          });
          return;
        }
        post({
          jsonrpc: "2.0",
          id: data.id,
          result: {},
        });
        return;
      }

      // Notifications.
      switch (data.method) {
        case "ui/notifications/tool-result": {
          const p = extractResult(data.params);
          if (p) setPayload(p);
          break;
        }
        default:
          break;
      }
    };

    window.addEventListener("message", onMessage);

    // Signal readiness. The host withholds tool-result until this arrives.
    post({ jsonrpc: "2.0", method: "ui/notifications/initialized" });

    return () => {
      window.removeEventListener("message", onMessage);
    };
  }, []);

  // Report height so the host can size the iframe.
  useEffect(() => {
    if (window.parent === window) return;
    if (!rootRef.current) return;

    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) return;
      const h = Math.ceil(entry.contentRect.height) + 32;
      if (h !== height) {
        setHeight(h);
        post({
          jsonrpc: "2.0",
          method: "ui/notifications/size-changed",
          params: { height: h },
        });
      }
    });

    observer.observe(rootRef.current);
    return () => observer.disconnect();
  }, [height]);

  return (
    <div ref={rootRef}>
      {payload ? (
        <div className="app">
          {payload.title ? <h2 className="title">{payload.title}</h2> : null}
          <Mermaid source={payload.diagram} />
        </div>
      ) : (
        <div className="placeholder">Waiting for diagram…</div>
      )}
    </div>
  );
}
