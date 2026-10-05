import { useEffect, useState } from "react";
import { alreadyMovedTo, recordMoveTo } from "@/lib/organization-host";

/**
 * Sends the browser to target (from organizationHostRedirectTarget) once per
 * tab and host. Returns true while the move is under way, so the caller can
 * hold a pending screen instead of painting the app it is about to leave. A
 * tab that already moved to that host, or that has no session storage to
 * record the move in, stays put.
 */
export function useOrganizationHostMove(target: string | undefined): boolean {
  const [moving, setMoving] = useState<string>();
  const [skipped, setSkipped] = useState<string>();

  useEffect(() => {
    if (!target || target === moving || target === skipped) return;
    if (alreadyMovedTo(target) || !recordMoveTo(target)) {
      setSkipped(target);
      return;
    }
    setMoving(target);
    window.location.replace(target);
  }, [target, moving, skipped]);

  return (
    target !== undefined &&
    target !== skipped &&
    (target === moving || !alreadyMovedTo(target))
  );
}
