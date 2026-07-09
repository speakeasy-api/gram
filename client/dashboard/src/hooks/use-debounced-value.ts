import { useEffect, useState } from "react";

/**
 * Returns a debounced copy of `value` that updates `delayMs` after the last
 * change settles. Use to defer expensive downstream work (search filters,
 * network requests) triggered by fast-changing input state.
 */
export function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debouncedValue, setDebouncedValue] = useState(value);

  useEffect(() => {
    const timeoutId = setTimeout(() => {
      setDebouncedValue(value);
    }, delayMs);

    return () => clearTimeout(timeoutId);
  }, [value, delayMs]);

  return debouncedValue;
}
