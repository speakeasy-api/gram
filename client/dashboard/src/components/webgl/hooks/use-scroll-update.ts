import type { RefObject } from "react";
import { useCallback, useEffect } from "react";
import { CANVAS_PADDING } from "../constants";
import { useWebGLStore } from "../store";

export const useScrollUpdate = (
  containerRef: RefObject<HTMLDivElement | null>,
) => {
  const scrollOffset = useWebGLStore((state) => state.scrollOffset);

  const updateContainerPosition = useCallback(() => {
    if (!containerRef.current) return;

    const scrollableHeight =
      document.documentElement.scrollHeight - containerRef.current.clientHeight;

    // Dont update if canvas hit windows bottom
    if (window.scrollY < scrollableHeight) {
      scrollOffset.set(
        window.scrollX,
        window.scrollY - window.innerHeight * CANVAS_PADDING,
      );

      containerRef.current.style.transform = `translate3d(${scrollOffset.x}px, ${scrollOffset.y}px, 0)`;
    }
  }, [containerRef, scrollOffset]);

  useEffect(() => {
    window.addEventListener("scroll", updateContainerPosition, {
      passive: true,
    });
    updateContainerPosition();

    return () => window.removeEventListener("scroll", updateContainerPosition);
  }, [updateContainerPosition]);
};
