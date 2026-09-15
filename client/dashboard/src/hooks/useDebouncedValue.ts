import { useEffect, useState } from "react";

/**
 * The value as it stood once `delayMs` passed without it changing. A burst
 * of edits settles to one debounced value; the first render returns the
 * value as given so consumers never see an undefined settle.
 */
export function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);

  return debounced;
}
