import { cn } from "@/lib/utils";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { AnimatePresence, motion } from "framer-motion";
import { createContext, useContext, useEffect, useRef, useState } from "react";

interface ToolResult {
  success?: boolean;
  reason?: string;
  modifiedSpec?: string;
  error?: unknown;
}

interface ToolInvocation {
  toolName: string;
  toolCallId: string;
  args: Record<string, unknown>;
  state: "partial-call" | "call" | "result";
  result?: ToolResult;
}

interface ToolCallPartProps {
  toolInvocation: ToolInvocation;
  addToolResult?: (params: { toolCallId: string; result: any }) => void;
}

interface ToolCallContextValue {
  isExpanded: boolean;
  setIsExpanded: (value: boolean) => void;
  state: "partial-call" | "call" | "result";
  hasError?: boolean;
  success?: boolean;
  canceled?: boolean;
}

const ToolCallContext = createContext<ToolCallContextValue | null>(null);

function useToolCallContext() {
  const context = useContext(ToolCallContext);
  if (!context) {
    throw new Error("Tool Call components must be used within a ToolCall root");
  }
  return context;
}

// Root component
interface ToolCallRootProps {
  children: React.ReactNode;
  state: "partial-call" | "call" | "result";
  hasError?: boolean;
  success?: boolean;
  canceled?: boolean;
}

function ToolCallRoot({
  children,
  state,
  hasError,
  success,
  canceled,
}: ToolCallRootProps) {
  const [isExpanded, setIsExpanded] = useState(false);

  return (
    <ToolCallContext.Provider
      value={{ isExpanded, setIsExpanded, state, hasError, success, canceled }}
    >
      <motion.div
        initial={{ opacity: 0, y: 5 }}
        animate={{ opacity: 1, y: 0 }}
        className="overflow-hidden rounded-md border"
      >
        {children}
      </motion.div>
    </ToolCallContext.Provider>
  );
}

// Header component
interface ToolCallHeaderProps {
  title: string;
  onAccept?: () => void;
  onReject?: () => void;
}

function ToolCallHeader({ title, onAccept, onReject }: ToolCallHeaderProps) {
  const { state, hasError, success, canceled, isExpanded, setIsExpanded } =
    useToolCallContext();

  return (
    <div className="flex h-8 items-center justify-between gap-2 px-2">
      <div className="flex items-center gap-2">
        <div className="flex h-4 w-4 items-center justify-center">
          {state === "partial-call" ? (
            <div className="size-3 animate-spin rounded-full border border-current border-t-transparent opacity-40" />
          ) : state === "result" ? (
            success ? (
              <Icon name="check" className="size-3 text-green-500" />
            ) : hasError ? (
              <Icon name="x" className="size-3 text-red-500" />
            ) : (
              <Icon name="slash" className="size-3 text-gray-400" />
            )
          ) : (
            <Icon name="git-branch" className="size-3 text-blue-500" />
          )}
        </div>
        <span className="text-xs font-medium">
          {state === "partial-call"
            ? "Preparing changes..."
            : state === "call"
              ? title
              : success
                ? "Changes applied"
                : hasError
                  ? "Error applying changes"
                  : canceled
                    ? "Changes canceled"
                    : "Changes ready"}
        </span>
      </div>

      <div className="flex items-center gap-1">
        {state === "call" && onAccept && onReject && (
          <>
            <button
              onClick={onAccept}
              className="text-muted-foreground hover:text-foreground rounded p-1 transition-colors"
            >
              <Icon name="check" className="size-3" />
            </button>
            <button
              onClick={onReject}
              className="text-muted-foreground hover:text-foreground rounded p-1 transition-colors"
            >
              <Icon name="x" className="size-3" />
            </button>
          </>
        )}
        <button
          onClick={() => setIsExpanded(!isExpanded)}
          className="text-muted-foreground hover:text-foreground rounded p-1 transition-colors"
        >
          <Icon
            name={isExpanded ? "minimize-2" : "maximize-2"}
            className="size-3"
          />
        </button>
      </div>
    </div>
  );
}

// Content component
interface ToolCallContentProps {
  children: React.ReactNode;
}

function ToolCallContent({ children }: ToolCallContentProps) {
  const { isExpanded } = useToolCallContext();

  if (!isExpanded) return null;

  return (
    <motion.div
      initial={{ height: 0, opacity: 0 }}
      animate={{ height: "auto", opacity: 1 }}
      exit={{ height: 0, opacity: 0 }}
      className="bg-muted/50 border-t"
    >
      {children}
    </motion.div>
  );
}

// Preview component for any content
interface ToolCallPreviewProps {
  children: React.ReactNode;
}

function ToolCallPreview({ children }: ToolCallPreviewProps) {
  return <div className="max-h-[300px] overflow-auto p-3">{children}</div>;
}

// Export the compound component
export const ToolCall = {
  Root: ToolCallRoot,
  Header: ToolCallHeader,
  Content: ToolCallContent,
  Preview: ToolCallPreview,
};

// Example usage of the new API:
export function AIChatToolCallPart({
  toolInvocation,
  addToolResult,
}: ToolCallPartProps) {
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

    const resObj = result as ToolResult | undefined;
    const success = resObj?.success === true;
    const canceled = resObj?.success === false && !resObj?.error;

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
          {reason && (
            <ToolCall.Preview>
              <div className="text-muted-foreground mb-2 text-xs font-medium">
                Reason for Changes
              </div>
              <div className="text-xs">{reason}</div>
            </ToolCall.Preview>
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
