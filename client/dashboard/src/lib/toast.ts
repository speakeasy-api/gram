import { toast as sonnerToast, type ExternalToast } from "sonner";
import type { Gram } from "@gram/client";
import type { ReactNode } from "react";

type NotificationLevel = "info" | "success" | "warning" | "error";
type NotificationType = "system" | "user_action";

interface ToastOptions extends ExternalToast {
  persist?: boolean;
  notificationType?: NotificationType;
  resourceType?: string;
  resourceId?: string;
}

let sdkClient: Gram | null = null;

export function initializeToastClient(client: Gram) {
  sdkClient = client;
}

export function clearToastClient() {
  sdkClient = null;
}

async function persistNotification(
  level: NotificationLevel,
  title: string,
  options?: ToastOptions
) {
  if (options?.persist !== true || !sdkClient) {
    return;
  }

  try {
    await sdkClient.notifications.create({
      createNotificationForm: {
        type: options?.notificationType ?? "user_action",
        level,
        title,
        resourceType: options?.resourceType,
        resourceId: options?.resourceId,
      },
    });
  } catch (e) {
    console.warn("Failed to persist notification:", e);
  }
}

function isStringMessage(message: string | ReactNode): message is string {
  return typeof message === "string";
}

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

  dismiss: sonnerToast.dismiss,
  promise: sonnerToast.promise,
  custom: sonnerToast.custom,
  message: sonnerToast.message,
  loading: sonnerToast.loading,
};

export default toast;
