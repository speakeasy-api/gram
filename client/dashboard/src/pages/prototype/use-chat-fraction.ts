import { useCallback, useEffect, useRef, useState } from "react";

const STORAGE_KEY = "prototype:chat-fraction";
const DEFAULT_FRACTION = 0.3;
/** The chat never gets narrower than this, so replies stay readable. */
const MIN_CHAT_PX = 320;
/** The surface never gets narrower than this, so pages keep their layout. */
const MIN_SURFACE_PX = 480;

function readStored(): number {
  try {
    const value = Number(window.localStorage.getItem(STORAGE_KEY));
    return value > 0 && value < 1 ? value : DEFAULT_FRACTION;
  } catch {
    return DEFAULT_FRACTION;
  }
}

/**
 * The chat's share of the row, adjustable by dragging the divider or with
 * the arrow keys. Clamped so both the chat and the surface stay usable; the
 * choice is remembered per browser.
 */
export function useChatFraction(): {
  fraction: number;
  containerRef: React.RefObject<HTMLDivElement | null>;
  handleProps: React.HTMLAttributes<HTMLDivElement> & {
    "aria-valuenow": number;
  };
  dragging: boolean;
} {
  const [fraction, setFraction] = useState(readStored);
  const [dragging, setDragging] = useState(false);
  const containerRef = useRef<HTMLDivElement | null>(null);

  const clamp = useCallback((value: number) => {
    const width = containerRef.current?.clientWidth ?? 1440;
    const min = MIN_CHAT_PX / width;
    const max = 1 - MIN_SURFACE_PX / width;
    return Math.min(Math.max(value, min), Math.max(min, max));
  }, []);

  useEffect(() => {
    try {
      window.localStorage.setItem(STORAGE_KEY, String(fraction));
    } catch {
      // Storage unavailable: the split just isn't remembered.
    }
  }, [fraction]);

  const onPointerMove = useCallback(
    (event: PointerEvent) => {
      const rect = containerRef.current?.getBoundingClientRect();
      if (!rect) return;
      setFraction(clamp((event.clientX - rect.left) / rect.width));
    },
    [clamp],
  );

  useEffect(() => {
    if (!dragging) return;
    const stop = () => setDragging(false);
    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", stop);
    return () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", stop);
    };
  }, [dragging, onPointerMove]);

  return {
    fraction,
    containerRef,
    dragging,
    handleProps: {
      role: "separator",
      "aria-orientation": "vertical",
      "aria-label": "Resize chat and page",
      "aria-valuenow": Math.round(fraction * 100),
      tabIndex: 0,
      onPointerDown: (event) => {
        event.preventDefault();
        setDragging(true);
      },
      onDoubleClick: () => setFraction(clamp(DEFAULT_FRACTION)),
      onKeyDown: (event) => {
        if (event.key === "ArrowLeft") setFraction((f) => clamp(f - 0.02));
        if (event.key === "ArrowRight") setFraction((f) => clamp(f + 0.02));
      },
    },
  };
}
