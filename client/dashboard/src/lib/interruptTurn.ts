import { getServerURL } from "@/lib/utils";

/**
 * Stops whatever a conversation is currently generating.
 *
 * The composer's stop button aborts the client's stream, which is all a
 * browser can do on its own — the assistant runs on a server-side runtime that
 * has never heard of this tab. Without this call, stopping only hides a reply
 * that keeps generating, keeps calling tools, and keeps spending tokens until
 * it finishes; the abandoned text then reappears in the transcript on reload.
 *
 * Called from an `abort` listener, so it takes no abort signal of its own:
 * the request must outlive the turn it is cancelling.
 *
 * Posted with `fetch` rather than through the generated SDK for the same
 * reason `turnStream` does — see `lib/turnStream.ts`. The route authenticates
 * from headers, so the caller's `Gram-Session` has to ride along.
 */
export async function interruptTurn(args: {
  chatId: string;
  assistantId: string;
  projectSlug: string;
  sessionToken?: string;
}): Promise<void> {
  const response = await fetch(
    `${getServerURL()}/rpc/assistants.interruptTurn`,
    {
      method: "POST",
      credentials: "include",
      headers: {
        "content-type": "application/json",
        "Gram-Project": args.projectSlug,
        ...(args.sessionToken ? { "Gram-Session": args.sessionToken } : {}),
      },
      body: JSON.stringify({
        assistant_id: args.assistantId,
        chat_id: args.chatId,
      }),
    },
  );

  if (!response.ok) {
    throw new Error(`interrupt failed: ${response.status}`);
  }
}
