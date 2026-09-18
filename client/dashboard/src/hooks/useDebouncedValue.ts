import { useEffect, useState } from "react";

/**
 * The value as it stood once `delayMs` passed without it changing. A burst
 * of edits settles to one debounced value; the first render returns the
 * value as given so consumers never see an undefined settle.
 */
export function useDebouncedValue<T>(value: T, delayMs: number): T {
  // Both wrapped in functions: React reads a bare function as a lazy
  // initializer and as a setter updater, so a function-valued T would be
  // called instead of stored.
  const [debounced, setDebounced] = useState<T>(() => value);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(() => value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);

  return debounced;
}
