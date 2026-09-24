// oxlint-disable-next-line no-restricted-imports -- approved unmount-only effect boundary
import { useEffect, useRef } from "react";

/** Run the latest cleanup callback exactly once when this component unmounts. */
export function useOnUnmount(cleanup: () => void): void {
  const cleanupRef = useRef(cleanup);
  const pendingRef = useRef<{ cancelled: boolean } | null>(null);
  cleanupRef.current = cleanup;

  useEffect(() => {
    if (pendingRef.current) pendingRef.current.cancelled = true;

    return () => {
      const pending = { cancelled: false };
      pendingRef.current = pending;
      queueMicrotask(() => {
        if (!pending.cancelled) cleanupRef.current();
        if (pendingRef.current === pending) pendingRef.current = null;
      });
    };
  }, []);
}
