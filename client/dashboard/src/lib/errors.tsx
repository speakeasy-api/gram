import { Type } from "@/components/ui/type";
import { Stack } from "@/components/ui/moonshine";
import { toast } from "sonner";

interface ErrorHandlerOptions {
  title?: string;
  persist?: boolean;
  customAction?: {
    label: string;
    onClick: () => void;
  };
  silent?: boolean;
}

export function toError(error: unknown): Error {
  if (error instanceof Error) return error;
  if (typeof error === "string") return new Error(error);
  if (error == null) return new Error("Unknown error");
  if (typeof error === "symbol" || typeof error === "bigint") {
    return new Error(String(error));
  }
  try {
    return new Error(JSON.stringify(error));
  } catch {
    return new Error("Unknown error");
  }
}

export function handleError(
  error: unknown,
  options: ErrorHandlerOptions = {},
): void {
  const {
    title = "Error",
    persist = false,
    customAction,
    silent = false,
  } = options;

  const errorMessage =
    typeof error === "string" ? error : toError(error).message;

  // Log error for debugging
  console.error("Error handled:", error);

  // Show toast notification unless silent
  if (!silent) {
    toast.error(
      <Stack gap={1}>
        <Type variant="subheading" className="text-destructive!">
          {title}
        </Type>
        <Type small muted>
          {errorMessage}
        </Type>
      </Stack>,
      {
        duration: persist ? Infinity : 5000,
        action: customAction
          ? {
              label: customAction.label,
              onClick: customAction.onClick,
            }
          : undefined,
      },
    );
  }
}

export function handleAPIError(error: unknown, defaultMessage?: string): void {
  let errorMessage = defaultMessage || "An unexpected error occurred";

  if (error instanceof Error) {
    errorMessage = error.message;
  } else if (typeof error === "string") {
    errorMessage = error;
  } else if (error && typeof error === "object") {
    // Handle API response errors
    if ("message" in error && typeof error.message === "string") {
      errorMessage = error.message;
    } else if ("error" in error && typeof error.error === "string") {
      errorMessage = error.error;
    }
  }

  handleError(errorMessage);
}
