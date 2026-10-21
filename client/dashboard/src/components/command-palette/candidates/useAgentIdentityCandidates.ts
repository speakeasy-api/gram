import {
  useIsPlatformAdmin,
  useOrganization,
  useSession,
} from "@/contexts/Auth";
import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { encodeIdentityUrn, withIdentityWindow } from "@/lib/identity-urn";
import { fetchRegisteredAgents } from "@/pages/identities/identityRoster";
import { useRoutes } from "@/routes";
import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { useLocation, useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

/**
 * Registered agents, opening the same canonical identity pages as the identity
 * roster. Agent names are identities too, so these candidates remain fuzzy-only
 * and are never sent to the intent service.
 */
export function useAgentIdentityCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const organization = useOrganization();
  const session = useSession();
  const isPlatformAdmin = useIsPlatformAdmin();
  const sdk = useSdkClient();
  const projectSlug = useProjectSlugForRequests();
  const routes = useRoutes({ projectSlug });
  const navigate = useNavigate();
  const { search } = useLocation();

  // Keep this aligned with the registered-agent query on the identities index
  // and the agent-management page. Those APIs deliberately reject support,
  // impersonated, demo, and RBAC-override sessions.
  const queryEnabled =
    enabled &&
    organization.slug !== DEMO_ORG_SLUG &&
    !session.organizationOverride &&
    !session.impersonatorEmail &&
    getRBACScopeOverrideHeader(import.meta.env.DEV || isPlatformAdmin) === null;

  const { data } = useQuery({
    // This is the identities index's key, so opening Command K after that page
    // reuses its inventory instead of issuing a duplicate list request.
    queryKey: [
      "identities",
      "registered-agents",
      organization.id,
      session.user.id,
    ],
    queryFn: ({ signal }) => fetchRegisteredAgents(sdk, signal),
    enabled: queryEnabled,
    throwOnError: false,
    retry: false,
  });

  return useMemo(() => {
    if (!queryEnabled) return [];
    return (data ?? []).map((agent): LauncherCandidate => {
      const urn = `agent:${agent.id}`;
      return {
        id: `agent-identity:${agent.id}`,
        // Identity names share the same privacy treatment as people.
        kind: "person",
        title: agent.name,
        detail: "Agent identity",
        keywords: ["agent", agent.id, urn],
        verbs: ["open"],
        fuzzyOnly: true,
        icon: "bot",
        group: "Identities",
        run: () => {
          void navigate(
            withIdentityWindow(
              routes.identities.detail.overview.href(encodeIdentityUrn(urn)),
              search,
            ),
          );
        },
      };
    });
  }, [data, navigate, queryEnabled, routes, search]);
}
