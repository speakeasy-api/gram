import { createContext, use } from "react";

/**
 * How a page hands work to the assistant. The prototype route provides one
 * that sends the prompt to its chat; Storybook and the classic dashboard get
 * the default, which reports that no assistant is attached.
 */
export interface ChatBridge {
  available: boolean;
  ask: (prompt: string) => void;
}

const ChatBridgeContext = createContext<ChatBridge>({
  available: false,
  ask: () => {},
});

export const ChatBridgeProvider = ChatBridgeContext.Provider;

export function useChatBridge(): ChatBridge {
  return use(ChatBridgeContext);
}

/** Fills `{title}` in an action's prompt with the record's name. */
export function fillPrompt(prompt: string, title: string): string {
  // A replacer keeps `$&` and similar sequences in the title verbatim.
  return prompt.replaceAll("{title}", () => title);
}
