import { useRegisterChatTelemetry, useTelemetry } from "@/contexts/Telemetry";
import { UIMessage } from "ai";
import { createContext, useContext, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { v7 as uuidv7 } from "uuid";

type AppendFn = (message: { content: string }) => void;

const ChatContext = createContext<{
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

export const useChatContext = () => {
  return useContext(ChatContext);
};

export const useChatMessages = () => {
  return useChatContext().messages;
};

export const ChatProvider = ({ children }: { children: React.ReactNode }) => {
  const telemetry = useTelemetry();
  const [searchParams] = useSearchParams();
  const [id, setId] = useState<string>(searchParams.get("chatId") ?? uuidv7());
  const [messages, setMessages] = useState<UIMessage[]>([]);

  const appendMessageFn = useRef<AppendFn>(() => {
    console.error("appendMessage is not set");
  });

  const url = new URL(window.location.href);
  url.searchParams.set("chatId", id);
  const urlString = url.toString();

  useRegisterChatTelemetry({
    chatId: id,
    chatUrl: urlString,
  });

  // This means a chat was explicitly loaded
  const doSetId = (id: string) => {
    setId(id);
    telemetry.capture("chat_event", {
      action: "chat_loaded",
      num_messages: messages.length,
      chat_id: id,
    });
  };

  return (
    <ChatContext.Provider
      value={{
        id,
        setId: doSetId,
        messages,
        setMessages,
        url: urlString,
        appendMessage: appendMessageFn.current,
        setAppendMessage: (appendMessage) => {
          appendMessageFn.current = appendMessage;
        },
      }}
    >
      {children}
    </ChatContext.Provider>
  );
};
