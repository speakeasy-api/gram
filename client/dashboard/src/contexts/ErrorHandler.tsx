import { Type } from "@/components/ui/type";
import { Stack } from "@speakeasy-api/moonshine";
import { createContext, ReactNode, useCallback, useContext } from "react";
import { toast } from "sonner";

interface ErrorHandlerContextType {
  handleError: (error: Error | string, options?: ErrorHandlerOptions) => void;
  showError: (error: Error | string, options?: ErrorHandlerOptions) => void;
}

interface ErrorHandlerOptions {
  title?: string;
  persist?: boolean;
  customAction?: {
    label: string;
    onClick: () => void;
  };
  silent?: boolean;
}

const ErrorHandlerContext = createContext<ErrorHandlerContextType | undefined>(
  undefined
);

export const useErrorHandler = () => {
  const context = useContext(ErrorHandlerContext);
  if (!context) {
    throw new Error(
      "useErrorHandler must be used within an ErrorHandlerProvider"
    );
  }
  return context;
};

interface ErrorHandlerProviderProps {
  children: ReactNode;
}

export const ErrorHandlerProvider = ({
  children,
}: ErrorHandlerProviderProps) => {
  const handleError = useCallback(
    (error: Error | string, options: ErrorHandlerOptions = {}) => {
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
          }
        );
      }
    },
    []
  );

  const showError = useCallback(
    (error: Error | string, options: ErrorHandlerOptions = {}) => {
      handleError(error, options);
    },
    [handleError]
  );

  const value = {
    handleError,
    showError,
  };

  return (
    <ErrorHandlerContext.Provider value={value}>
      {children}
    </ErrorHandlerContext.Provider>
  );
};
