import { useOrganization } from "@/contexts/Auth";
import { useCallback, useRef } from "react";

export function useIsCurrentOrganization(
  organizationId: string,
): () => boolean {
  const activeOrganizationId = useOrganization().id;
  const activeOrganizationIdRef = useRef(activeOrganizationId);
  activeOrganizationIdRef.current = activeOrganizationId;

  return useCallback(
    () => activeOrganizationIdRef.current === organizationId,
    [organizationId],
  );
}
