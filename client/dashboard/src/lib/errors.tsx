import { Text } from "@/components/ui/Text";
import { Stack } from "@/components/ui/Stack";
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

// isNotFoundError reports whether an SDK call failed with a 404. Some lookups
// treat "not found" as a normal answer rather than a failure — asking whether a
// record exists before creating it — so they catch the rejection and branch on
// this instead of surfacing an error.
export function isNotFoundError(error: unknown): boolean {
  if (!(error instanceof Error)) return false;
  return "statusCode" in error && error.statusCode === 404;
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
        <Text variant="subheading" className="text-destructive!">
          {title}
        </Text>
        <Text small muted>
          {errorMessage}
        </Text>
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
