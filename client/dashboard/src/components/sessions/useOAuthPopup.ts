import { useEffect, useRef, useState } from "react";

/** Re-read connections when OAuth closes or the dashboard regains focus. */
export function useOAuthPopup(onComplete: () => void | Promise<void>): {
  open: (url: string) => boolean;
  blocked: boolean;
} {
  const callback = useRef(onComplete);
  callback.current = onComplete;
  const popup = useRef<Window | null>(null);
  const [blocked, setBlocked] = useState(false);
  useEffect(() => {
    const refresh = () => {
      if (popup.current && !popup.current.closed) return;
      if (document.visibilityState === "visible") void callback.current();
    };
    const closed = window.setInterval(() => {
      if (popup.current?.closed) {
        popup.current = null;
        void callback.current();
      }
    }, 500);
    window.addEventListener("focus", refresh);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      window.clearInterval(closed);
      window.removeEventListener("focus", refresh);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, []);
  return {
    blocked,
    open: (url) => {
      const child = window.open(
        "about:blank",
        "speakeasy-connection",
        "popup=yes,width=560,height=720",
      );
      setBlocked(!child);
      if (!child) return false;
      child.opener = null;
      child.location.href = url;
      popup.current = child;
      child.focus();
      return true;
    },
  };
}
