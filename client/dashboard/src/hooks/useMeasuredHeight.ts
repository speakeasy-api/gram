import { useCallback, useLayoutEffect, useState } from "react";

/** Ref callback + live height (px) of the element it's attached to, kept in
 * sync via `ResizeObserver` so callers can size an unrelated element (e.g. an
 * absolutely-positioned overlay) to match. `undefined` until the first
 * measurement lands.
 *
 * A callback ref (backed by state), not a plain `useRef` object — the target
 * element may mount later than this hook's owner (e.g. behind a loading
 * spinner), and a plain ref's `useLayoutEffect` with an empty dependency
 * array would only ever see it as `null`, since nothing re-renders the owner
 * when `.current` changes out from under it. */
export function useMeasuredHeight<T extends HTMLElement>(): {
  ref: (node: T | null) => void;
  height: number | undefined;
} {
  const [node, setNode] = useState<T | null>(null);
  const [height, setHeight] = useState<number | undefined>(undefined);
  const ref = useCallback((el: T | null) => setNode(el), []);

  useLayoutEffect(() => {
    if (!node) {
      setHeight(undefined);
      return;
    }
    // Re-measure via getBoundingClientRect on every callback rather than
    // trusting ResizeObserver's own entry: `entry.contentRect` is the
    // content box (padding/border excluded), which would silently shrink
    // the reported height relative to the initial border-box measurement
    // below.
    const measure = () => setHeight(node.getBoundingClientRect().height);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [node]);

  return { ref, height };
}
