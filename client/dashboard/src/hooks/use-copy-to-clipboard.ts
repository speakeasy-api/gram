import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Writes text to the clipboard and exposes a `copied` flag that stays true
 * for `resetMs` after a successful copy — the timed-checkmark state used by
 * copy buttons throughout the dashboard (see `CopyButton`).
 */
export function useCopyToClipboard(resetMs = 1500): {
  copied: boolean;
  copy: (text: string) => Promise<void>;
} {
  const [copied, setCopied] = useState(false);
  const resetTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (resetTimeoutRef.current) clearTimeout(resetTimeoutRef.current);
    };
  }, []);

  const copy = useCallback(
    async (text: string): Promise<void> => {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      if (resetTimeoutRef.current) clearTimeout(resetTimeoutRef.current);
      resetTimeoutRef.current = setTimeout(() => {
        setCopied(false);
        resetTimeoutRef.current = null;
      }, resetMs);
    },
    [resetMs],
  );

  return { copied, copy };
}
