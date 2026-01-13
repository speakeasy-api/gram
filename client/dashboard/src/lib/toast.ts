import { toast as sonnerToast, type ExternalToast } from "sonner";
import type { Gram } from "@gram/client";
import type { ReactNode } from "react";

type NotificationLevel = "info" | "success" | "warning" | "error";
type NotificationType = "system" | "user_action";

interface ToastOptions extends ExternalToast {
  /** Whether to persist this notification to the backend. Defaults to true for string messages. */
  persist?: boolean;
  /** The type of notification. Defaults to "user_action". */
  notificationType?: NotificationType;
  /** Optional resource type this notification relates to */
  resourceType?: string;
  /** Optional resource ID this notification relates to */
  resourceId?: string;
}

let sdkClient: Gram | null = null;

/**
 * Initialize the toast wrapper with an SDK client.
 * This must be called before toasts can be persisted to the backend.
 */
export function initializeToastClient(client: Gram) {
  sdkClient = client;
}

/**
 * Clear the SDK client (e.g., on logout).
 */
export function clearToastClient() {
  sdkClient = null;
}

async function persistNotification(
  level: NotificationLevel,
  title: string,
  options?: ToastOptions
) {
  if (options?.persist === false || !sdkClient) {
    return;
  }

  try {
    await sdkClient.notifications.create({
      type: options?.notificationType ?? "user_action",
      level,
      title,
      resourceType: options?.resourceType,
      resourceId: options?.resourceId,
    });
  } catch (e) {
    // Silently fail - we don't want notification persistence to break the app
    console.warn("Failed to persist notification:", e);
  }
}

function isStringMessage(message: string | ReactNode): message is string {
  return typeof message === "string";
}

/**
 * Toast wrapper that persists string notifications to the backend.
 *
 * Usage:
 * - `toast.success("Message")` - Shows toast and persists to backend
 * - `toast.error(<Component />)` - Shows toast only (JSX cannot be persisted)
 * - `toast.info("Message", { persist: false })` - Shows toast without persisting
 */
export const toast = {
  success: (message: string | ReactNode, options?: ToastOptions) => {
    sonnerToast.success(message, options);
    if (isStringMessage(message)) {
      persistNotification("success", message, options);
    }
  },

  error: (message: string | ReactNode, options?: ToastOptions) => {
    sonnerToast.error(message, options);
    if (isStringMessage(message)) {
      persistNotification("error", message, options);
    }
  },

  warning: (message: string | ReactNode, options?: ToastOptions) => {
    sonnerToast.warning(message, options);
    if (isStringMessage(message)) {
      persistNotification("warning", message, options);
    }
  },

  info: (message: string | ReactNode, options?: ToastOptions) => {
    sonnerToast.info(message, options);
    if (isStringMessage(message)) {
      persistNotification("info", message, options);
    }
  },

  // Passthrough methods that don't need persistence
  dismiss: sonnerToast.dismiss,
  promise: sonnerToast.promise,
  custom: sonnerToast.custom,
  message: sonnerToast.message,
  loading: sonnerToast.loading,
};

export default toast;
