import { cn } from "@/lib/utils";
import { motion } from "framer-motion";
import "./styles.css";
import type { Message } from "ai";
import { useState } from "react";
import { Icon } from "@speakeasy-api/moonshine";
import { ToolCall } from "./AIChatToolCallPart.tsx";

interface MessagePart {
  type: "text" | "reasoning" | "tool-invocation";
  text?: string;
  reasoning?: string;
  toolInvocation?: {
    toolName: string;
    toolCallId: string;
    args: Record<string, unknown>;
    state: "partial-call" | "call" | "result";
    result?: any;
  };
}

interface AIChatMessageBubbleProps {
  message: Message & {
    data?: {
      isStreaming?: boolean;
    };
    parts?: MessagePart[];
  };
  isStreaming?: boolean;
  // We'll need these to confirm/cancel tool calls:
  addToolResult: (params: { toolCallId: string; result: any }) => void;
  applyModifiedSpec: (newSpec: string) => void;
}

function TextPart({
  text,
  isStreaming,
}: {
  text: string;
  isStreaming?: boolean;
}) {
  return (
    <div
      className={cn(
        "pr-8",
        isStreaming &&
          "relative after:absolute after:bottom-0 after:left-0 after:h-1 after:w-full after:animate-pulse after:bg-gradient-to-r after:from-transparent after:via-blue-500/20 after:to-transparent"
      )}
    >
      {text}
    </div>
  );
}

function ReasoningPart({
  reasoning,
  isStreaming,
}: {
  reasoning: string;
  isStreaming?: boolean;
}) {
  const [isExpanded, setIsExpanded] = useState(false);

  return (
    <div className="flex flex-col gap-2">
      <button
        onClick={() => setIsExpanded(!isExpanded)}
        className="text-muted-foreground hover:text-foreground flex items-center gap-2 text-left text-sm"
      >
        <Icon
          name={isExpanded ? "chevron-down" : "chevron-right"}
          className="size-4"
        />
        <span className="font-medium">Reasoning</span>
      </button>

      {isExpanded ? (
        <motion.div
          initial={{ height: 0, opacity: 0 }}
          animate={{ height: "auto", opacity: 1 }}
          exit={{ height: 0, opacity: 0 }}
          className="bg-muted/50 overflow-hidden rounded-md p-3 text-sm"
        >
          <div className="whitespace-pre-wrap">{reasoning}</div>
        </motion.div>
      ) : (
        <div className="text-muted-foreground text-sm">
          {reasoning.split("\n")[0]}...
        </div>
      )}
    </div>
  );
}

function ToolInvocationPart({
  toolInvocation,
  addToolResult,
}: {
  toolInvocation: NonNullable<MessagePart["toolInvocation"]>;
  addToolResult: (params: { toolCallId: string; result: any }) => void;
}) {
  const { toolName, toolCallId, args, state, result } = toolInvocation;
  const hasError =
    result &&
    typeof result === "object" &&
    result !== null &&
    "error" in result;

  if (toolName === "modifySpec") {
    const { modifiedSpec, reason } =
      (args as { modifiedSpec?: string; reason?: string }) ?? {};

    const handleAccept = () => {
      console.log("handleAccept", modifiedSpec);
      if (addToolResult) {
        addToolResult({
          toolCallId,
          result: {
            success: true,
            reason,
            modifiedSpec,
          },
        });
      }
    };

    const handleReject = () => {
      if (addToolResult) {
        addToolResult({
          toolCallId,
          result: {
            success: false,
            reason: reason || "User canceled",
            modifiedSpec: null,
          },
        });
      }
    };

    const success = result?.success === true;
    const canceled = result?.success === false && !hasError;

    return (
      <ToolCall.Root
        state={state}
        hasError={hasError}
        success={success}
        canceled={canceled}
      >
        <ToolCall.Header
          title="Review Changes"
          onAccept={handleAccept}
          onReject={handleReject}
        />
        <ToolCall.Content>
          {modifiedSpec && (
            <ToolCall.Diff
              original="// Original spec..." // TODO: We need to get the original spec from somewhere
              modified={modifiedSpec}
            />
          )}
        </ToolCall.Content>
      </ToolCall.Root>
    );
  }

  // Fallback for other tool types
  return (
    <ToolCall.Root state={state} hasError={hasError}>
      <ToolCall.Header title={toolName} />
      <ToolCall.Content>
        <ToolCall.Preview>
          <pre className="text-xs">{JSON.stringify(args, null, 2)}</pre>
        </ToolCall.Preview>
      </ToolCall.Content>
    </ToolCall.Root>
  );
}

export function AIChatMessageBubble({
  message,
  isStreaming,
  addToolResult,
}: AIChatMessageBubbleProps) {
  const isActuallyStreaming = isStreaming || message.data?.isStreaming;

  return (
    <motion.div
      initial={{ opacity: 0, y: 10 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.2, ease: "easeOut" }}
      className="flex items-start gap-3 px-4 py-2"
    >
      <div
        className={cn(
          "h-8 w-8 flex-shrink-0 rounded-lg",
          message.role === "assistant" ? "bg-purple-600" : "bg-blue-600"
        )}
      />
      <div className="flex max-w-prose flex-1 flex-col gap-3">
        {/* Handle messages with parts */}
        {message.parts?.map((part, index) => {
          switch (part.type) {
            case "text":
              return (
                <TextPart
                  key={index}
                  text={part.text || ""}
                  isStreaming={
                    isActuallyStreaming && index === message.parts!.length - 1
                  }
                />
              );
            case "reasoning":
              return (
                <ReasoningPart
                  key={index}
                  reasoning={part.reasoning || ""}
                  isStreaming={
                    isActuallyStreaming && index === message.parts!.length - 1
                  }
                />
              );
            case "tool-invocation":
              return part.toolInvocation ? (
                <ToolInvocationPart
                  key={index}
                  toolInvocation={part.toolInvocation}
                  addToolResult={addToolResult}
                />
              ) : null;
            default:
              return null;
          }
        })}

        {/* Backwards compatibility for messages without parts */}
        {message.content && !message.parts && (
          <TextPart text={message.content} isStreaming={isActuallyStreaming} />
        )}
      </div>
    </motion.div>
  );
}
