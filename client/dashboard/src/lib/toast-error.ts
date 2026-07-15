import { toast } from "sonner";

/**
 * Shows an error toast: the error's own message when `error` is an `Error`
 * instance, otherwise `fallback`. Centralizes the
 * `err instanceof Error ? err.message : "..."` pattern repeated across catch
 * blocks throughout the dashboard.
 */
export function toastError(error: unknown, fallback: string): void {
  toast.error(error instanceof Error ? error.message : fallback);
}
