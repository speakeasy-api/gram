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
    // Resolved once at startup; opencode reloads plugins on config change, so
    // the MCP server set is stable for the life of this instance.
    mcpServers: await loadMcpServerNames(client, directory),
  };

  // Assistant text streams in over several message.part.updated events
  // before the message is marked complete; buffer the latest full text per
  // messageID and flush it once, on completion. Cleared on session end to
  // bound memory (ponytail: whole-map clear, not per-message eviction — a
  // single plugin instance is scoped to one opencode session/directory, so
  // this is enough).
  const assistantText = new Map<string, string>();

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
            assistantText.set(part.messageID, part.text);
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
            const text = assistantText.get(info.id) ?? "";
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
      void send(toolRequested(input, output.args, ctx));
    },
    "tool.execute.after": async (input, output) => {
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
