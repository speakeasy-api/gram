import { cn } from "@/lib/utils";
import { Icon } from "@speakeasy-api/moonshine";
import { useState } from "react";

interface ToolCallProps {
  name: string;
  status: "running" | "complete" | "error";
  args?: Record<string, any>;
  result?: any;
  error?: string;
}

export function AIChatToolCall({
  name,
  status,
  args,
  result,
  error,
}: ToolCallProps) {
  const [isExpanded, setIsExpanded] = useState(false);

  return (
    <div className="bg-muted/50 flex flex-col gap-2 rounded-md border p-3 text-sm">
      <div className="flex items-center gap-2">
        <div
          className={cn(
            "size-4 flex-shrink-0 rounded-full",
            status === "running" && "bg-blue-500",
            status === "complete" && "bg-green-500",
            status === "error" && "bg-red-500",
          )}
        >
          {status === "running" && (
            <div className="size-full animate-spin rounded-full border-2 border-white/20 border-t-white" />
          )}
          {status === "complete" && (
            <Icon name="check" className="size-4 text-white" />
          )}
          {status === "error" && (
            <Icon name="x" className="size-4 text-white" />
          )}
        </div>
        <div className="font-medium">{name}</div>
        <button
          onClick={() => setIsExpanded(!isExpanded)}
          className="text-muted-foreground hover:text-foreground ml-auto"
        >
          <Icon
            name={isExpanded ? "chevron-up" : "chevron-down"}
            className="size-4"
          />
        </button>
      </div>

      {isExpanded && (
        <div className="flex flex-col gap-2 pl-6">
          {args && (
            <div>
              <div className="text-muted-foreground mb-1 text-xs">
                Arguments
              </div>
              <pre className="whitespace-pre-wrap text-xs">
                {JSON.stringify(args, null, 2)}
              </pre>
            </div>
          )}
          {(result || error) && (
            <div>
              <div className="text-muted-foreground mb-1 text-xs">
                {error ? "Error" : "Result"}
              </div>
              <pre className="whitespace-pre-wrap text-xs">
                {error || JSON.stringify(result, null, 2)}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
