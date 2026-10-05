import { useEffect, useState } from "react";
import { alreadyMoved, moveKey, recordMove } from "@/lib/organization-host";

/**
 * Sends the browser to target (from organizationHostRedirectTarget) once per
 * tab, organization and host. Returns true while the move is under way, so
 * the caller can hold a pending screen instead of painting the app it is about
 * to leave. A tab that already moved this organization to that host, or that
 * has no session storage to record the move in, stays put.
 */
export function useOrganizationHostMove(
  organizationId: string | undefined,
  target: string | undefined,
): boolean {
  const key =
    organizationId && target ? moveKey(organizationId, target) : undefined;
  const [handled, setHandled] = useState<{ key: string; moving: boolean }>();

  useEffect(() => {
    if (!key || !target || handled?.key === key) return;
    const moving = !alreadyMoved(key) && recordMove(key);
    setHandled({ key, moving });
    if (moving) window.location.replace(target);
  }, [key, target, handled]);

  if (!key) return false;
  return handled?.key === key ? handled.moving : !alreadyMoved(key);
}
