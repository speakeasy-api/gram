import {
  useIsPlatformAdmin,
  useOrganization,
  useSession,
} from "@/contexts/Auth";
import { getRBACScopeOverrideHeader } from "@/components/dev-toolbar-utils";
import {
  useProjectSlugForRequests,
  useSdkClient,
  useSlugs,
} from "@/contexts/Sdk";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { encodeIdentityUrn, withIdentityWindow } from "@/lib/identity-urn";
import { fetchRegisteredAgents } from "@/pages/identities/identityRoster";
import { useRoutes } from "@/routes";
import { useInfiniteQuery, useQueries } from "@tanstack/react-query";
import { useEffect, useMemo } from "react";
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
  const { projectSlug: routeProjectSlug } = useSlugs();
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

  const {
    data,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    isError,
  } = useInfiniteQuery({
    // This is the identities index's key, so opening Command K after that page
    // reuses its inventory instead of issuing a duplicate list request.
    queryKey: [
      "identities",
      "registered-agents",
      organization.id,
      session.user.id,
      "",
      "asc",
      "[]",
      "[]",
      "",
    ],
    queryFn: ({ pageParam, signal }) =>
      fetchRegisteredAgents(
        sdk,
        { cursor: pageParam, limit: 50, search: "", sortOrder: "asc" },
        signal,
      ),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    enabled: queryEnabled,
    throwOnError: false,
    retry: false,
  });

  // The inventory is cursor-paginated; loading just its first page would make
  // older or later-named agents disappear from global name search.
  useEffect(() => {
    if (
      !queryEnabled ||
      !hasNextPage ||
      isFetchingNextPage ||
      isFetchNextPageError
    )
      return;
    void fetchNextPage().catch(() => undefined);
  }, [
    queryEnabled,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  ]);

  const credentialAgents = useMemo(
    () =>
      queryEnabled && routeProjectSlug && !isError
        ? (data?.pages ?? [])
            .flatMap((page) => page.items)
            .filter((agent) => agent.permissions?.authorize)
        : [],
    [data, queryEnabled, routeProjectSlug, isError],
  );
  const keyQueries = useQueries({
    queries: credentialAgents.map((agent) => ({
      queryKey: [
        "agent-key-search",
        organization.id,
        session.user.id,
        agent.id,
      ],
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        sdk.keys.list(
          { agentId: agent.id },
          { sessionHeaderGramSession: "" },
          { signal },
        ),
      enabled: queryEnabled,
      retry: false,
      throwOnError: false,
    })),
  });

  return useMemo(() => {
    if (!queryEnabled) return [];
    const keysByAgent = new Map(
      credentialAgents.map((agent, index) => [agent.id, keyQueries[index]]),
    );
    return (data?.pages ?? [])
      .flatMap((page) => page.items)
      .flatMap((agent): LauncherCandidate[] => {
        const urn = `agent:${agent.id}`;
        const identity: LauncherCandidate = {
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
        // Request headers may fall back to the default project in the org shell;
        // only a real project route can offer this project-scoped control.
        const candidates = [identity];
        if (!routeProjectSlug || isError) return candidates;
        if (agent.permissions?.authorize) {
          const openCredentials = () => {
            void navigate(
              withIdentityWindow(
                routes.identities.detail.credentials.href(
                  encodeIdentityUrn(urn),
                ),
                search,
              ),
            );
          };
          candidates.push({
            ...identity,
            id: `agent-credentials:${agent.id}`,
            title: `${agent.name} · Credentials`,
            detail: "Review API keys and sign-in methods",
            keywords: [...identity.keywords, "credentials", "keys", "reissue"],
            run: openCredentials,
          });
          const keyQuery = keysByAgent.get(agent.id);
          if (keyQuery?.isSuccess) {
            for (const key of keyQuery.data.keys) {
              candidates.push({
                ...identity,
                id: `agent-key:${key.id}`,
                title: key.name,
                detail: `${agent.name} · API key`,
                keywords: ["key", "credential", "reissue", agent.name],
                run: openCredentials,
              });
            }
          }
        }
        if (agent.permissions?.write)
          candidates.push({
            ...identity,
            id: `agent-inference-access:${agent.id}`,
            title: `${agent.name} · Permissions`,
            detail: "Edit permissions · inference project access",
            keywords: [
              ...identity.keywords,
              "inference",
              "inference:execute",
              "permissions",
            ],
            run: () => {
              void navigate(
                withIdentityWindow(
                  routes.identities.detail.permissions.href(
                    encodeIdentityUrn(urn),
                  ),
                  search,
                ),
              );
            },
          });
        return candidates;
      });
  }, [
    data,
    navigate,
    queryEnabled,
    routes,
    routeProjectSlug,
    search,
    isError,
    keyQueries,
    credentialAgents,
  ]);
}
