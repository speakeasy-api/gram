import type { ToolActivitySummarizer } from "@/elements";
import { getServerURL } from "@/lib/utils";

/**
 * createToolActivitySummarizer returns a {@link ToolActivitySummarizer} that
 * calls the `chat.summarizeToolActivity` management endpoint to turn a turn's
 * tool calls into a short, human-readable "task" label for the chat UI.
 *
 * It mirrors the dashboard SDK's request auth — session cookie
 * (`credentials: "include"`) plus the `gram-project` header — so it works
 * wherever the dashboard is authenticated. On any failure it returns `null`,
 * letting Elements fall back to its instant heuristic label.
 */
export function createToolActivitySummarizer(
  projectSlug: string,
): ToolActivitySummarizer {
  return async ({ toolCalls, userMessage, inProgress, signal }) => {
    if (toolCalls.length === 0) return null;

    try {
      const response = await fetch(
        `${getServerURL()}/rpc/chat.summarizeToolActivity`,
        {
          method: "POST",
          credentials: "include",
          signal,
          headers: {
            "Content-Type": "application/json",
            "gram-project": projectSlug,
          },
          body: JSON.stringify({
            tool_calls: toolCalls.map((call) => ({
              name: call.name,
              arguments: call.arguments,
            })),
            user_message: userMessage,
            in_progress: inProgress,
          }),
        },
      );

      if (!response.ok) return null;

      const data = (await response.json()) as { summary?: string };
      const summary = data.summary?.trim();
      return summary && summary.length > 0 ? summary : null;
    } catch {
      // Aborted (turn moved on) or network/auth error — heuristic label stands.
      return null;
    }
  };
}
