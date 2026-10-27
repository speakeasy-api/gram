import { useEffect, useRef, useState } from "react";

/** Re-read connections when OAuth closes or the dashboard regains focus. */
export function useOAuthPopup(
  onComplete: (reason: "popup-closed" | "focus") => void | Promise<void>,
): {
  open: (url: string | (() => Promise<string>)) => boolean;
  blocked: boolean;
} {
  const callback = useRef(onComplete);
  callback.current = onComplete;
  const popup = useRef<Window | null>(null);
  const inFlight = useRef(0);
  const preparing = useRef(false);
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
      if (preparing.current || !popup.current?.closed) return false;
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
      if (popup.current) {
        if (!popup.current.closed) popup.current.focus();
        return false;
      }
      const child = window.open(
        "about:blank",
        "speakeasy-connection",
        "popup=yes,width=560,height=720",
      );
      setBlocked(!child);
      if (!child) return false;
      child.opener = null;
      popup.current = child;
      if (typeof url === "string") {
        child.location.href = url;
      } else {
        // Reserve the window during the click, before asynchronous setup.
        preparing.current = true;
        void url()
          .then((destination) => {
            if (!child.closed) child.location.href = destination;
            preparing.current = false;
          })
          .catch(() => {
            preparing.current = false;
            popup.current = null;
            child.close();
          });
      }
      child.focus();
      return true;
    },
  };
}
