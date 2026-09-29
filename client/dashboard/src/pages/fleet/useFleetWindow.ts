import { useCallback, useEffect, useMemo, useState } from "react";
import { FLEET_WINDOW_MS } from "./fleet-model";

/** A shared rolling clock keeps server pagination and both Fleet views in range. */
export function useFleetWindow(): {
  now: number;
  from: Date;
  refreshWindow: () => void;
} {
  const [now, setNow] = useState(Date.now);
  const refreshWindow = useCallback(() => {
    if (document.visibilityState !== "hidden") setNow(Date.now());
  }, []);
  useEffect(() => {
    const timer = window.setInterval(refreshWindow, 30_000);
    window.addEventListener("focus", refreshWindow);
    document.addEventListener("visibilitychange", refreshWindow);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("focus", refreshWindow);
      document.removeEventListener("visibilitychange", refreshWindow);
    };
  }, [refreshWindow]);
  const from = useMemo(() => new Date(now - FLEET_WINDOW_MS), [now]);
  return { now, from, refreshWindow };
}
