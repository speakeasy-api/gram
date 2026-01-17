import { Type } from "@/components/ui/type";
import { Stack } from "@speakeasy-api/moonshine";
import { toast } from "@/lib/toast";

interface ErrorHandlerOptions {
  title?: string;
  persist?: boolean;
  customAction?: {
    label: string;
    onClick: () => void;
  };
  silent?: boolean;
}

export function handleError(
  error: Error | string,
  options: ErrorHandlerOptions = {},
) {
  const {
    title = "Error",
    persist = false,
    customAction,
    silent = false,
  } = options;

  const errorMessage = typeof error === "string" ? error : error.message;

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

export function handleAPIError(error: unknown, defaultMessage?: string) {
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
