import { z } from "zod";
import {
  gatewayRPC,
  type GatewayRPCExchange,
} from "../gateway/useGatewayInspection";

const PROTOCOL_VERSION = "2026-07-28";
const SESSION_HEADER = "mcp-session-id";
const TRACE_HEADER = "x-trace-id";

const callResultSchema = z
  .object({
    content: z
      .array(
        z
          .object({ type: z.string().optional(), text: z.string().optional() })
          .passthrough(),
      )
      .optional(),
    structuredContent: z.unknown().optional(),
    isError: z.boolean().optional(),
  })
  .passthrough();

export interface McpToolRun {
  /** The tool ran and reported success. */
  ok: boolean;
  /** The text a client would show, or the error the tool or server reported. */
  summary: string | undefined;
  /** structuredContent when present, otherwise the whole result. */
  value: unknown;
  durationMs: number;
  /** The trace the server recorded for this call, when it said. */
  traceId: string | undefined;
  /** The tools/call request and the decoded response, as sent on the wire. */
  exchange: GatewayRPCExchange | undefined;
}

/**
 * Runs one tool exactly as a client would: `initialize`, then `tools/call`.
 * The session id from `initialize` rides along for servers that keep state;
 * stateless servers ignore it. Every failure comes back as a run, not a throw,
 * so the panel can always show what happened.
 */
export async function runMcpTool({
  url,
  headers,
  name,
  args,
  signal,
}: {
  url: string;
  headers: Record<string, string> | undefined;
  name: string;
  args: Record<string, unknown>;
  signal?: AbortSignal;
}): Promise<McpToolRun> {
  const startedAt = performance.now();
  let exchange: GatewayRPCExchange | undefined;
  const elapsed = () => Math.round(performance.now() - startedAt);
  const traceId = () => exchange?.headers.get(TRACE_HEADER) ?? undefined;

  try {
    let sessionId: string | undefined;
    await gatewayRPC(
      url,
      headers,
      "initialize",
      {
        protocolVersion: PROTOCOL_VERSION,
        capabilities: {},
        clientInfo: { name: "speakeasy-dashboard-inspect", version: "1" },
      },
      signal,
      undefined,
      (init) => {
        sessionId = init.headers.get(SESSION_HEADER) ?? undefined;
      },
    );
    const callHeaders = sessionId
      ? { ...headers, [SESSION_HEADER]: sessionId }
      : headers;

    const raw = await gatewayRPC(
      url,
      callHeaders,
      "tools/call",
      { name, arguments: args },
      signal,
      undefined,
      (observed) => {
        exchange = observed;
      },
    );
    const result = callResultSchema.parse(raw);
    const text = result.content
      ?.map((part) => part.text)
      .filter((part): part is string => !!part)
      .join("\n");
    return {
      ok: !result.isError,
      summary: text || undefined,
      value: result.structuredContent ?? parseJson(text) ?? result,
      durationMs: elapsed(),
      traceId: traceId(),
      exchange,
    };
  } catch (error) {
    return {
      ok: false,
      summary:
        error instanceof Error ? error.message : "The call could not be read.",
      value: undefined,
      durationMs: elapsed(),
      traceId: traceId(),
      exchange,
    };
  }
}

/** Tools often return JSON as text; show it as JSON when it is. */
function parseJson(text: string | undefined): unknown {
  if (!text) return undefined;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return undefined;
  }
}
