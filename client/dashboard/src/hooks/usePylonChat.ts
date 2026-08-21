import { useCallback, useEffect, useSyncExternalStore } from "react";

import {
  bindPylonChatListeners,
  isPylonChatOpen,
  subscribePylonChatOpen,
  togglePylonChat,
} from "@/lib/pylon";

/**
 * Shared Pylon chat visibility. `onShow` / `onHide` from the widget keep
 * every consumer (account menu, other Get Support buttons) in sync.
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
