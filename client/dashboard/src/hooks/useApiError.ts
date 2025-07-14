import { useErrorHandler } from "@/contexts/ErrorHandler";
import { useCallback } from "react";

export const useApiError = () => {
  const { handleError } = useErrorHandler();

  const handleApiError = useCallback((error: unknown, defaultMessage?: string) => {
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
  }, [handleError]);

  return { handleApiError };
};