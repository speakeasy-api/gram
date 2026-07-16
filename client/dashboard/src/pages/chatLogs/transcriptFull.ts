import type { Gram } from "@gram/client";
import type { ChatMessage } from "@gram/client/models/components/chatmessage.js";

// The server's max chat.load page (maxLoadChatLimit), so full-range walks
// complete in as few round trips as possible.
export const FULL_LOAD_PAGE_SIZE = 200;

/** Every message of the chat's latest generation, walked by seq keyset from
 * the start until the server reports nothing newer. */
export async function fetchFullTranscript(
  client: Gram,
  chatId: string,
): Promise<ChatMessage[]> {
  const messages: ChatMessage[] = [];
  let page = await client.chat.load({
    id: chatId,
    fromStart: true,
    limit: FULL_LOAD_PAGE_SIZE,
  });
  messages.push(...page.messages);
  while (page.hasMoreAfter && page.messages.length > 0) {
    const newest = page.messages[page.messages.length - 1]!;
    page = await client.chat.load({
      id: chatId,
      afterSeq: newest.seq,
      limit: FULL_LOAD_PAGE_SIZE,
    });
    messages.push(...page.messages);
  }
  return messages;
}
