// oxlint-disable-next-line no-restricted-imports -- approved unmount-only effect boundary
import { useEffect, useRef } from "react";

/** Run the latest cleanup callback exactly once when this component unmounts. */
export function useOnUnmount(cleanup: () => void): void {
  const cleanupRef = useRef(cleanup);
  cleanupRef.current = cleanup;

  useEffect(() => () => cleanupRef.current(), []);
}
