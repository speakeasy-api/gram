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
} from "./mapping.js";
import { send } from "./send.js";

const PLUGIN_VERSION = "0.1.0";

export const GramObservability: Plugin = async ({ directory, client }) => {
  const ctx: Ctx = {
    directory,
    fallbackSession: crypto.randomUUID(),
    adapterVersion: PLUGIN_VERSION,
    userEmail: process.env.GRAM_USER_EMAIL,
    mcpServers: [],
  };

  // Resolve the MCP server list lazily on the first tool event, not at
  // bootstrap: client.mcp.status() hits opencode's own server API, which isn't
  // serving yet during plugin init, so a bootstrap fetch fails fast and would
  // cache an empty list for the whole session. By the first tool call the
  // server is up. Memoized so it runs once; fire-and-forget so it never blocks
  // tool execution. The very first tool call may still see an empty list (load
  // in flight) and pass names through un-normalized — fail-open, exactly the
  // pre-normalization behavior.
  let mcpLoad: Promise<void> | undefined;
  const ensureMcpServers = (): void => {
    if (mcpLoad) return;
    mcpLoad = loadMcpServerNames(client, directory).then((names) => {
      ctx.mcpServers = names;
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

// The keys of the MCP status map are the configured server names (e.g.
// "context7"), which is what tool names are prefixed with. Fail-open: any
// error yields an empty list, so tool names pass through un-normalized —
// exactly the behavior before normalization existed.
async function loadMcpServerNames(
  client: Parameters<Plugin>[0]["client"],
  directory: string,
): Promise<readonly string[]> {
  try {
    const res = await client.mcp.status({ query: { directory } });
    return Object.keys(res.data ?? {});
  } catch {
    return [];
  }
}

export default GramObservability;
