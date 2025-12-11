import { UIMessage } from "ai";
import { cn } from "@/lib/utils";
import { Type } from "@/components/ui/type";
import { useEffect, useRef } from "react";

interface ChatMessagesProps {
  messages: UIMessage[];
  isLoading?: boolean;
  renderMessage?: (message: UIMessage) => React.ReactNode;
  className?: string;
}

export function ChatMessages({
  messages,
  isLoading,
  renderMessage,
  className,
}: ChatMessagesProps) {
  const messagesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages]);

  return (
    <div className={cn("flex flex-col gap-4 p-4 overflow-y-auto", className)}>
      {messages.map((message) => (
        <div key={message.id}>
          {renderMessage ? (
            renderMessage(message)
          ) : (
            <DefaultMessageRenderer message={message} />
          )}
        </div>
      ))}
      {isLoading && (
        <div className="flex items-center gap-2 text-muted-foreground">
          <div className="h-2 w-2 animate-bounce rounded-full bg-current [animation-delay:-0.3s]" />
          <div className="h-2 w-2 animate-bounce rounded-full bg-current [animation-delay:-0.15s]" />
          <div className="h-2 w-2 animate-bounce rounded-full bg-current" />
        </div>
      )}
      <div ref={messagesEndRef} />
    </div>
  );
}

function DefaultMessageRenderer({ message }: { message: UIMessage }) {
  return (
    <div
      className={cn(
        "flex flex-col gap-2 rounded-lg p-4",
        message.role === "user"
          ? "ml-auto max-w-[80%] bg-primary text-primary-foreground"
          : "mr-auto max-w-[80%] bg-muted",
      )}
    >
      <Type variant="small" className="font-medium opacity-70">
        {message.role === "user" ? "You" : "Assistant"}
      </Type>
      <div className="whitespace-pre-wrap">
        {message.parts.map((part, index) => {
          if (part.type === "text") {
            return <span key={index}>{part.text}</span>;
          }
          return null;
        })}
      </div>
    </div>
  );
}
