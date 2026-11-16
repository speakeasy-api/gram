import { useEffect, useState } from "react";

/**
 * The current time, refreshed every `intervalMs`, so labels that count down
 * to a deadline re-render without anything else changing.
 */
export function useNow(intervalMs: number): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const id = window.setInterval(() => setNow(new Date()), intervalMs);
    return () => window.clearInterval(id);
  }, [intervalMs]);
  return now;
}
