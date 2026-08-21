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
