import type { ToolActivitySummarizer } from "@/elements";
import { getServerURL } from "@/lib/utils";

/**
 * createToolActivitySummarizer returns a {@link ToolActivitySummarizer} that
 * calls the `chat.summarizeToolActivity` management endpoint to turn a turn's
 * tool calls into a short, human-readable "task" label for the chat UI.
 *
 * It mirrors the dashboard SDK's request auth — the session cookie
 * (`credentials: "include"`) plus the `gram-project` header — and additionally
 * forwards the session token as the `Gram-Session` header when one is provided,
 * so enrichment still works in cross-origin dev/preview setups where the
 * SameSite=Lax cookie isn't sent. On any failure it returns `null`, letting
 * Elements fall back to its instant heuristic label.
 */
export function createToolActivitySummarizer(
  projectSlug: string,
  sessionToken?: string,
): ToolActivitySummarizer {
  return async ({ toolCalls, userMessage, inProgress, signal }) => {
    if (toolCalls.length === 0) return null;

    const headers: Record<string, string> = {
      "Content-Type": "application/json",
      "gram-project": projectSlug,
    };
    if (sessionToken) {
      headers["Gram-Session"] = sessionToken;
    }

    try {
      const response = await fetch(
        `${getServerURL()}/rpc/chat.summarizeToolActivity`,
        {
          method: "POST",
          credentials: "include",
          signal,
          headers,
          body: JSON.stringify({
            // Arguments are scrubbed of detected secrets server-side before
            // they reach the summarizing model.
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
