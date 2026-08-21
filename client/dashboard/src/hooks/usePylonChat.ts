import { useCallback, useEffect, useSyncExternalStore } from "react";

import {
  bindPylonChatListeners,
  isPylonChatOpen,
  subscribePylonChatOpen,
  togglePylonChat,
} from "@/lib/pylon";

/**
 * Shared Pylon chat visibility. Widget `onShow` / `onHide` keep the
 * account-menu label in sync with the chat window.
 */
export function usePylonChat(): { isOpen: boolean; toggle: () => void } {
  useEffect(() => {
    // Best-effort: bind if window.Pylon already exists. AuthProvider usually
    // initializes Pylon after this child's effect, so this is often a no-op.
    // show/hide rebind before talking to the widget.
    bindPylonChatListeners();
  }, []);

  const isOpen = useSyncExternalStore(
    subscribePylonChatOpen,
    isPylonChatOpen,
    () => false,
  );

  const toggle = useCallback(() => {
    togglePylonChat();
  }, []);

  return { isOpen, toggle };
}
