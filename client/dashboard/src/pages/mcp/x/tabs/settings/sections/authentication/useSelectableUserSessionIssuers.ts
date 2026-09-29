import { useEffectiveUserSessionIssuers } from "@/hooks/useEffectiveUserSessionIssuers";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { useMemo } from "react";
import type { AuthTarget } from "./authTarget";

/**
 * The issuers this target may switch to, with its current issuer kept in the
 * list even when the effective listing would not otherwise include it.
 */
export function useSelectableUserSessionIssuers(target: AuthTarget): {
  issuers: UserSessionIssuer[];
  isLoading: boolean;
  isError: boolean;
} {
  const userSessionIssuerId = target.userSessionIssuerId ?? undefined;
  const effectiveIssuersQuery = useEffectiveUserSessionIssuers({
    mcpResourceId: target.permissionResourceId,
  });
  // The current issuer may be missing from the effective list, so it is read
  // on its own; until it lands the picker could show no current issuer and
  // offer a reassignment it cannot judge, so its state counts too.
  const currentIssuerQuery = useUserSessionIssuer(
    { id: userSessionIssuerId },
    undefined,
    { enabled: !!userSessionIssuerId, throwOnError: false },
  );
  const userSessionIssuer = currentIssuerQuery.data;
  const issuers = useMemo(() => {
    const supportedIssuers = target.supportsOrganizationIssuers
      ? effectiveIssuersQuery.issuers
      : effectiveIssuersQuery.issuers.filter(
          (issuer) => issuer.projectId !== "",
        );
    if (
      !userSessionIssuer ||
      (!target.supportsOrganizationIssuers &&
        userSessionIssuer.projectId === "") ||
      supportedIssuers.some((issuer) => issuer.id === userSessionIssuer.id)
    ) {
      return supportedIssuers;
    }
    return [userSessionIssuer, ...supportedIssuers];
  }, [
    effectiveIssuersQuery.issuers,
    target.supportsOrganizationIssuers,
    userSessionIssuer,
  ]);

  return {
    issuers,
    isLoading: effectiveIssuersQuery.isLoading || currentIssuerQuery.isLoading,
    isError: effectiveIssuersQuery.isError || currentIssuerQuery.isError,
  };
}
