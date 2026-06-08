import { UIMessage } from "ai";
import { createContext, useContext } from "react";

type AppendFn = (message: { content: string }) => void;

export const ChatContext = createContext<{
  id: string;
  setId: (id: string) => void;
  url: string;
  messages: UIMessage[];
  setMessages: (messages: UIMessage[]) => void;
  appendMessage: AppendFn;
  setAppendMessage: (appendMessage: AppendFn) => void;
}>({
  id: "",
  setId: () => {},
  url: "",
  messages: [],
  setMessages: () => {},
  appendMessage: () => {},
  setAppendMessage: () => {},
});

export const useChatContext = (): {
  id: string;
  setId: (id: string) => void;
  url: string;
  messages: UIMessage[];
  setMessages: (messages: UIMessage[]) => void;
  appendMessage: AppendFn;
  setAppendMessage: (appendMessage: AppendFn) => void;
} => {
  return useContext(ChatContext);
};

export const useChatMessages = (): UIMessage[] => {
  return useChatContext().messages;
};
