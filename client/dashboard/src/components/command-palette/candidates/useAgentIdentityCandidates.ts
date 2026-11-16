import { useOrganization, useSession } from "@/contexts/Auth";
import {
  useProjectSlugForRequests,
  useSdkClient,
  useSlugs,
} from "@/contexts/Sdk";
import { DEMO_ORG_SLUG } from "@/lib/demo";
import { useRBACScopeOverride } from "@/hooks/useRBACScopeOverride";
import { encodeIdentityUrn, withIdentityWindow } from "@/lib/identity-urn";
import { fetchRegisteredAgents } from "@/pages/identities/identityRoster";
import { useRoutes } from "@/routes";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef } from "react";
import { useLocation, useNavigate } from "react-router";
import type { LauncherCandidate } from "./types";

const AGENT_INVENTORY_STALE_TIME_MS = 30_000;

/**
 * Registered agents, opening the same canonical identity pages as the identity
 * roster. Candidates are matched locally by name and aliases.
 */
export function useAgentIdentityCandidates({
  enabled,
}: {
  enabled: boolean;
}): LauncherCandidate[] {
  const organization = useOrganization();
  const session = useSession();
  const scopeOverride = useRBACScopeOverride();
  const sdk = useSdkClient();
  const projectSlug = useProjectSlugForRequests();
  const { projectSlug: routeProjectSlug } = useSlugs();
  const routes = useRoutes({ projectSlug });
  const navigate = useNavigate();
  const { search } = useLocation();

  // Keep this aligned with the registered-agent query on the identities index
  // and the agent-management page. Those APIs deliberately reject support,
  // impersonated, and demo sessions.
  const queryEnabled =
    enabled &&
    organization.slug !== DEMO_ORG_SLUG &&
    !session.organizationOverride &&
    !session.impersonatorEmail;

  const {
    data,
    fetchNextPage,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
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
      scopeOverride,
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
    // Reopening the palette used to mark every loaded cursor page stale and
    // refetch the full inventory immediately. Keep a short freshness window so
    // quick close/reopen cycles reuse the complete inventory already fetched.
    staleTime: AGENT_INVENTORY_STALE_TIME_MS,
    throwOnError: false,
    retry: false,
  });

  // The inventory is cursor-paginated; loading just its first page would make
  // older or later-named agents disappear from global name search. A failed
  // page gets one retry on the next palette open without looping while open.
  const opens = useRef(0);
  const lastFetchOpen = useRef(0);
  useEffect(() => {
    if (queryEnabled) opens.current += 1;
  }, [queryEnabled]);
  useEffect(() => {
    if (!queryEnabled || !hasNextPage || isFetchingNextPage) return;
    if (isFetchNextPageError && lastFetchOpen.current === opens.current) return;
    lastFetchOpen.current = opens.current;
    void fetchNextPage().catch(() => undefined);
  }, [
    queryEnabled,
    hasNextPage,
    isFetchingNextPage,
    isFetchNextPageError,
    fetchNextPage,
  ]);

  return useMemo(() => {
    if (!queryEnabled) return [];
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
        // Where its keys are listed and issued. Found by the old tab name too.
        const signIn: LauncherCandidate = {
          ...identity,
          id: `agent-sign-in:${agent.id}`,
          title: `${agent.name} · Agent sign-in`,
          detail: "API keys · issue a key",
          keywords: [
            ...identity.keywords,
            "sign-in",
            "credentials",
            "api key",
            "key",
          ],
          run: () => {
            void navigate(
              withIdentityWindow(
                routes.identities.detail.credentials.href(
                  encodeIdentityUrn(urn),
                ),
                search,
              ),
            );
          },
        };
        if (!agent.permissions?.write) return [identity, signIn];
        // Where its servers are changed and an expired connection is fixed,
        // so "reconnect Linear for the release bot" lands on the tab for it.
        const mcpSignIn: LauncherCandidate = {
          ...identity,
          id: `agent-mcp-sign-in:${agent.id}`,
          title: `${agent.name} · MCP sign-in`,
          detail: "Change MCP servers · reconnect expired sign-ins",
          keywords: [
            ...identity.keywords,
            "mcp",
            "servers",
            "sign-in",
            "connection",
            "connections",
            "reconnect",
            "review",
            "change",
          ],
          run: () => {
            void navigate(
              withIdentityWindow(
                routes.identities.detail.mcpSignIn.href(encodeIdentityUrn(urn)),
                search,
              ),
            );
          },
        };
        // Request headers may fall back to the default project in the org shell;
        // only a real project route can offer this project-scoped control.
        if (!routeProjectSlug) return [identity, signIn, mcpSignIn];
        return [
          identity,
          signIn,
          mcpSignIn,
          {
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
          },
        ];
      });
  }, [data, navigate, queryEnabled, routes, routeProjectSlug, search]);
}
