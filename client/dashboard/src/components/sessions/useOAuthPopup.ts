import { useEffect, useRef, useState } from "react";

/** Re-read connections when OAuth closes or the dashboard regains focus. */
export function useOAuthPopup(
  onComplete: (reason: "popup-closed" | "focus") => void | Promise<void>,
): {
  open: (url: string) => boolean;
  blocked: boolean;
} {
  const callback = useRef(onComplete);
  callback.current = onComplete;
  const popup = useRef<Window | null>(null);
  const inFlight = useRef(0);
  const [blocked, setBlocked] = useState(false);
  useEffect(() => {
    const notify = async (reason: "popup-closed" | "focus") => {
      if (reason === "focus" && inFlight.current > 0) return;
      inFlight.current += 1;
      try {
        await callback.current(reason);
      } finally {
        inFlight.current -= 1;
      }
    };
    const consumeClosedPopup = () => {
      if (!popup.current?.closed) return false;
      popup.current = null;
      void notify("popup-closed");
      return true;
    };
    const refresh = () => {
      if (popup.current) {
        consumeClosedPopup();
        return;
      }
      if (document.visibilityState === "visible") void notify("focus");
    };
    const closed = window.setInterval(() => {
      consumeClosedPopup();
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
