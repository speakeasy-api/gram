import { useCallback, useEffect, useRef, useState } from "react";

const STORAGE_KEY = "prototype:chat-fraction";
const DEFAULT_FRACTION = 0.3;
/** The chat never gets narrower than this, so replies stay readable. */
const MIN_CHAT_PX = 320;
/** The surface never gets narrower than this, so pages keep their layout. */
const MIN_SURFACE_PX = 480;
/** Width of the drag handle between them (`w-4` in SplitHandle). */
const HANDLE_PX = 16;

/** The element's content box: its border box minus horizontal padding. */
function contentBox(el: HTMLElement): { left: number; width: number } {
  const rect = el.getBoundingClientRect();
  const style = window.getComputedStyle(el);
  const left = parseFloat(style.paddingLeft) || 0;
  const right = parseFloat(style.paddingRight) || 0;
  return { left: rect.left + left, width: rect.width - left - right };
}

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
  const [stored, setStored] = useState(readStored);
  const [dragging, setDragging] = useState(false);
  const [width, setWidth] = useState<number | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);

  // The chat's width is a percentage of the row's content box (inside its
  // padding), and the handle takes HANDLE_PX of that, so measure that box.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const measure = () => setWidth(contentBox(el).width);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const clamp = useCallback(
    (value: number) => {
      if (!width) return value;
      const min = MIN_CHAT_PX / width;
      const max = (width - HANDLE_PX - MIN_SURFACE_PX) / width;
      return Math.min(Math.max(value, min), Math.max(min, max));
    },
    [width],
  );
  // Clamp on every render too, so a remembered split that no longer fits
  // (a narrower window) never squeezes the surface below its minimum.
  const fraction = clamp(stored);

  useEffect(() => {
    try {
      window.localStorage.setItem(STORAGE_KEY, String(stored));
    } catch {
      // Storage unavailable: the split just isn't remembered.
    }
  }, [stored]);

  const onPointerMove = useCallback(
    (event: PointerEvent) => {
      const el = containerRef.current;
      if (!el) return;
      const box = contentBox(el);
      if (box.width <= 0) return;
      // Center the handle on the pointer.
      const chatPx = event.clientX - box.left - HANDLE_PX / 2;
      setStored(clamp(chatPx / box.width));
    },
    [clamp],
  );

  useEffect(() => {
    if (!dragging) return;
    const stop = () => setDragging(false);
    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", stop);
    window.addEventListener("pointercancel", stop);
    return () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
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
      onDoubleClick: () => setStored(clamp(DEFAULT_FRACTION)),
      onKeyDown: (event) => {
        if (event.key === "ArrowLeft") setStored(clamp(fraction - 0.02));
        if (event.key === "ArrowRight") setStored(clamp(fraction + 0.02));
      },
    },
  };
}
