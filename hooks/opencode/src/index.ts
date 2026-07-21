import { hostname } from "node:os";
import type { Plugin } from "@opencode-ai/plugin";
import {
  assistantResponded,
  permissionAsked,
  promptSubmitted,
  sessionEnded,
  sessionStarted,
  toolCompleted,
  toolFailed,
  toolRequested,
  type Ctx,
  type McpServer,
} from "./mapping.js";
import { send } from "./send.js";

const PLUGIN_VERSION = "0.1.0";

export const GramObservability: Plugin = async ({ directory, client }) => {
  const ctx: Ctx = {
    directory,
    fallbackSession: crypto.randomUUID(),
    adapterVersion: PLUGIN_VERSION,
    userEmail: process.env.GRAM_USER_EMAIL,
    // Best-effort; empty string (unusual) just omits the origin tier.
    hostname: hostname() || undefined,
    mcpServers: new Map(),
  };

  // Resolve the MCP server config lazily on the first tool event, not at
  // bootstrap: client.config.get() hits opencode's own server API, which isn't
  // serving yet during plugin init, so a bootstrap fetch fails fast and would
  // cache an empty map for the whole session. By the first tool call the server
  // is up. Memoized so it runs once; fire-and-forget so it never blocks tool
  // execution. The very first tool call may still see an empty map (load in
  // flight) and pass names through un-normalized — fail-open, exactly the
  // pre-normalization behavior.
  let mcpLoad: Promise<void> | undefined;
  const ensureMcpServers = (): void => {
    if (mcpLoad) return;
    mcpLoad = loadMcpServers(client, directory).then((servers) => {
      ctx.mcpServers = servers;
    });
  };

  // Assistant text streams in over several message.part.updated events before
  // the message is marked complete, and a single message may carry multiple
  // text parts (interleaved with tool calls). Buffer the latest text per part
  // id, keyed by messageID, and join them in arrival order on completion.
  // Cleared on session end to bound memory (ponytail: whole-map clear, not
  // per-message eviction — a single plugin instance is scoped to one opencode
  // session/directory, so this is enough).
  const assistantText = new Map<string, Map<string, string>>();

  return {
    event: async ({ event }) => {
      switch (event.type) {
        case "session.created":
          void send(sessionStarted(event.properties.info, ctx));
          break;
        case "session.idle":
          void send(
            sessionEnded(event.properties.sessionID, "session.idle", ctx),
          );
          assistantText.clear();
          break;
        case "session.deleted":
          void send(
            sessionEnded(event.properties.info.id, "session.deleted", ctx),
          );
          assistantText.clear();
          break;
        case "message.part.updated": {
          const part = event.properties.part;
          if (part.type === "text") {
            let parts = assistantText.get(part.messageID);
            if (!parts) {
              parts = new Map<string, string>();
              assistantText.set(part.messageID, parts);
            }
            parts.set(part.id, part.text);
          } else if (part.type === "tool" && part.state.status === "error") {
            void send(
              toolFailed(
                {
                  sessionID: part.sessionID,
                  callID: part.callID,
                  tool: part.tool,
                  state: part.state,
                },
                ctx,
              ),
            );
          }
          break;
        }
        case "message.updated": {
          const info = event.properties.info;
          if (info.role === "assistant" && info.time?.completed) {
            const parts = assistantText.get(info.id);
            const text = parts ? [...parts.values()].join("\n") : "";
            assistantText.delete(info.id);
            void send(assistantResponded(info, text, ctx));
          }
          break;
        }
        default:
          break;
      }
    },
    "chat.message": async (input, output) => {
      const text = textFromParts(output.parts);
      void send(promptSubmitted({ sessionID: input.sessionID }, text, ctx));
    },
    "tool.execute.before": async (input, output) => {
      ensureMcpServers();
      void send(toolRequested(input, output.args, ctx));
    },
    "tool.execute.after": async (input, output) => {
      ensureMcpServers();
      void send(toolCompleted(input, output, ctx));
    },
    "permission.ask": async (input) => {
      void send(
        permissionAsked(
          {
            id: input.id,
            sessionID: input.sessionID,
            type: input.type,
            callID: input.callID,
          },
          ctx,
        ),
      );
    },
  };
};

function textFromParts(
  parts: ReadonlyArray<{ type: string; text?: string }>,
): string {
  return parts
    .filter((part) => part.type === "text")
    .map((part) => part.text ?? "")
    .join("\n");
}

// opencode's config.mcp maps each configured server name (e.g. "context7") —
// which is what tool names are prefixed with — to its transport config: remote
// servers carry a url, local (stdio) servers carry a command array. Both feed
// the ingest payload's data.mcp block so the server can resolve gram-hosted vs
// shadow. Fail-open: any error yields an empty map, so tool names pass through
// un-normalized — exactly the behavior before normalization existed.
async function loadMcpServers(
  client: Parameters<Plugin>[0]["client"],
  directory: string,
): Promise<ReadonlyMap<string, McpServer>> {
  const servers = new Map<string, McpServer>();
  try {
    const res = await client.config.get({ query: { directory } });
    for (const [name, cfg] of Object.entries(res.data?.mcp ?? {})) {
      if (cfg.type === "remote") {
        servers.set(name, { url: cfg.url });
      } else if (cfg.type === "local") {
        servers.set(name, { command: cfg.command.join(" ") });
      } else {
        servers.set(name, {});
      }
    }
  } catch {
    return new Map();
  }
  return servers;
}

export default GramObservability;
