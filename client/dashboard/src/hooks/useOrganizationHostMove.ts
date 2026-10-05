import { useEffect, useState } from "react";
import { alreadyMovedTo, recordMoveTo } from "@/lib/organization-host";

/**
 * Sends the browser to target (from organizationHostRedirectTarget) once per
 * tab, organization and host. Returns true while the move is under way, so
 * the caller can hold a pending screen instead of painting the app it is about
 * to leave. A tab that already moved this organization to that host, or that
 * has no session storage to record the move in, stays put.
 */
export function useOrganizationHostMove(
  organizationId: string,
  target: string | undefined,
): boolean {
  const key = target ? `${organizationId} ${target}` : undefined;
  const [moving, setMoving] = useState<string>();
  const [skipped, setSkipped] = useState<string>();

  useEffect(() => {
    if (!target || !key || key === moving || key === skipped) return;
    if (
      alreadyMovedTo(organizationId, target) ||
      !recordMoveTo(organizationId, target)
    ) {
      setSkipped(key);
      return;
    }
    setMoving(key);
    window.location.replace(target);
  }, [organizationId, target, key, moving, skipped]);

  return (
    target !== undefined &&
    key !== skipped &&
    (key === moving || !alreadyMovedTo(organizationId, target))
  );
}
