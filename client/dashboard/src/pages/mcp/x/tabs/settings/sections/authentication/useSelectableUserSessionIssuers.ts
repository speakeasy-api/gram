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
  const { data: userSessionIssuer } = useUserSessionIssuer(
    { id: userSessionIssuerId },
    undefined,
    { enabled: !!userSessionIssuerId },
  );
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
    isLoading: effectiveIssuersQuery.isLoading,
    isError: effectiveIssuersQuery.isError,
  };
}
