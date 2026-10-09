import { useEffect } from "react";

// Tells the dialog hosting a delete whether it is running, so the dialog can
// refuse to close. Reports idle on unmount, so a body swapped out mid-run
// never leaves the dialog locked.
export function useReportBusy(
  busy: boolean,
  onBusyChange: ((busy: boolean) => void) | undefined,
): void {
  useEffect(() => {
    onBusyChange?.(busy);
    return () => onBusyChange?.(false);
  }, [busy, onBusyChange]);
}
