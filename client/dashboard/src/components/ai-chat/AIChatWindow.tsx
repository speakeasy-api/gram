import { Icon, Input, Text } from "@speakeasy-api/moonshine";
import "./styles.css";
import { Button } from "@speakeasy-api/moonshine";
import { useState, useRef, useEffect } from "react";
import { AIChatSuggestions } from "./AIChatSuggestions.tsx";
import { AIChatMessageBubble } from "./AIChatMessageBubble.tsx";
import { type Message } from "ai";
import { AIChatSuggestion } from "./types.ts";
import { Textarea } from "./Textarea.tsx";
import { cn } from "@/lib/utils";

export interface AIChatWindowProps {
  children?: React.ReactNode;
}

const AIChatWindow = ({ children }: AIChatWindowProps) => {
  return (
    <div className="flex h-full flex-col gap-2">
      <div className="bg-muted flex flex-row items-center gap-1.5 border-b p-2.5">
        <Icon
          name="bot"
          className="size-7 fill-primary dark:fill-primary-foreground"
          strokeWidth={1.4}
        />
        <Text className="font-medium leading-none">Speakeasy AI</Text>
      </div>
      {children}
    </div>
  );
};

AIChatWindow.displayName = "AIChatWindow";

export interface AIChatConversationProps {
  messages: Message[];
  isGenerating?: boolean;
  addToolResult: (toolCallId: string, result: any) => void;
}

export interface AIChatPromptProps {
  onSend: (message: string) => void;
  disabled?: boolean;
  onStop?: () => void;
  onRegenerate?: () => void;
  onSuggestionClick: (suggestion: AIChatSuggestion) => void;
}

const defaultSuggestions: AIChatSuggestion[] = [
  {
    text: "Add missing examples",
    emoji: "👍",
    fullPrompt: `
  Please add missing examples to every operation in the OpenAPI specification.
This includes adding examples to request and response bodies.
Do not make any other changes to the operations except adding example blocks.
  `,
  },
  {
    text: "Deduplicate inline schemas",
    emoji: "🌍",
    fullPrompt: `
  Please deduplicate inline schemas in the OpenAPI specification.
  This includes deduplicating schemas that are identical in content.
  Do not make any other changes to the operations except deduplicating schemas.
  The act of deduplicating schemas involves removing inline schemas and moving them to the components section.
  `,
  },
  {
    text: "Add tags to operations",
    emoji: "🏷️",
    fullPrompt: `Please add tags to every operation in the OpenAPI specification which is missing a tag.
  The tags added to each operation should be relevant to the name of the operation based on its path or operationId.
  `,
  },
  {
    text: "Add missing operationIds",
    emoji: "🔑",
    fullPrompt: `Please add an operationId property to every operation in the OpenAPI specification which is missing an operationId.
  The operationId should be a unique identifier for the operation based on its path and method.
  `,
  },
];

const AIChatPrompt = ({
  onSend,
  onSuggestionClick,
  disabled,
}: AIChatPromptProps) => {
  const [prompt, setPrompt] = useState("");
  const [isFocused, setIsFocused] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Add auto-growing functionality
  const adjustHeight = () => {
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
      textareaRef.current.style.height = `${textareaRef.current.scrollHeight}px`;
    }
  };

  const handlePromptChange = (
    event: React.ChangeEvent<HTMLTextAreaElement>
  ) => {
    setPrompt(event.target.value);
    adjustHeight();
  };

  const handleSend = () => {
    if (prompt.trim() && !disabled) {
      onSend(prompt);
      setPrompt("");
      if (textareaRef.current) {
        textareaRef.current.style.height = "auto";
      }
    }
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      handleSend();
    }
  };

  // Initialize height on mount
  useEffect(() => {
    adjustHeight();
  }, []);

  return (
    <div className="mt-auto flex flex-col p-2">
      <div className="flex flex-row items-center gap-2">
        <AIChatSuggestions
          suggestions={defaultSuggestions}
          onSuggestionClick={onSuggestionClick}
        />
      </div>
      <div className="flex flex-col">
        <div
          className={cn(
            "flex flex-col rounded-md border transition-shadow",
            isFocused && "ring-2 ring-white ring-offset-1"
          )}
        >
          <Textarea
            ref={textareaRef}
            className="max-h-[300px] min-h-[44px] w-full resize-none bg-transparent"
            value={prompt}
            onChange={handlePromptChange}
            onKeyDown={handleKeyDown}
            onFocus={() => setIsFocused(true)}
            onBlur={() => setIsFocused(false)}
            placeholder="Send a message..."
            disabled={disabled}
            rows={1}
          />
          <div className="flex items-center justify-between p-3 pt-2">
            <div className="flex items-center gap-2">
              {/* Add any additional footer buttons here */}
            </div>
            <Button
              variant="secondary"
              size="icon"
              className="h-8 w-8 rounded-sm"
              onClick={handleSend}
              disabled={!prompt.trim() || disabled}
            >
              <Icon name="arrow-up" className="size-4" />
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
};

AIChatPrompt.displayName = "AIChatWindow.Prompt";

const AIChatConversation = ({
  messages,
  isGenerating,
  addToolResult,
}: AIChatConversationProps) => {
  return (
    <div className="flex flex-grow flex-col gap-2 p-3">
      {messages.map((message, index) => (
        <AIChatMessageBubble
          key={message.id || index}
          message={message}
          isStreaming={isGenerating && index === messages.length - 1}
          addToolResult={addToolResult}
        />
      ))}
    </div>
  );
};

AIChatConversation.displayName = "AIChatWindow.Conversation";

const AIChatWindowWithSubComponents = Object.assign(AIChatWindow, {
  Prompt: AIChatPrompt,
  Conversation: AIChatConversation,
});

export default AIChatWindowWithSubComponents;
