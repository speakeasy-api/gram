import {
  createElement,
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import { useSession } from "@/contexts/Auth";
import { useKillswitchAccess } from "@/hooks/useKillswitchAccess";
import { useBatchKillswitchUserBadgesMutation } from "@gram/client/react-query/batchKillswitchUserBadges.js";
import type { KillswitchUserBadge } from "@gram/client/models/components/killswitchuserbadge.js";
import { killswitchCreateHref } from "./killswitch-routing";

export { killswitchCreateHref };

const MAX_BADGE_USERS_PER_REQUEST = 100;
const MAX_CONCURRENT_BADGE_REQUESTS = 2;
export const BADGE_REFRESH_INTERVAL_MS = 60_000;
const EMPTY_BADGES = new Map<string, KillswitchUserBadge>();
const EMPTY_USER_IDS = new Set<string>();

type BadgeLoadResult = {
  badges: KillswitchUserBadge[];
  unavailableUserIds: string[];
};

const inFlightBadgeRequests = new Map<string, Promise<BadgeLoadResult>>();

export function canonicalUserId(subjectUrn: string): string | undefined {
  if (!subjectUrn.startsWith("user:")) return undefined;
  const userId = subjectUrn.slice(5);
  return userId !== "" && !userId.includes(":") ? userId : undefined;
}

export function killswitchStatusHref(baseHref: string, userId: string): string {
  const params = new URLSearchParams({ user: userId });
  return `${baseHref}?${params.toString()}`;
}

export function mcpSessionsUserHref(baseHref: string, userId: string): string {
  const params = new URLSearchParams({ subjectUrn: `user:${userId}` });
  return `${baseHref}?${params.toString()}`;
}

type BadgeLoaderProps = {
  loadKey: string;
  sessionToken: string;
  userIds: string[];
  onLoading: () => void;
  onLoaded: (result: BadgeLoadResult) => void;
};

function KillswitchUserBadgesLoader({
  loadKey,
  sessionToken,
  userIds,
  onLoading,
  onLoaded,
}: BadgeLoaderProps): null {
  const { mutateAsync: batchUserBadges } =
    useBatchKillswitchUserBadgesMutation();

  useEffect(() => {
    let current = true;
    onLoading();
    let request = inFlightBadgeRequests.get(loadKey);
    if (!request) {
      const chunks = Array.from(
        { length: Math.ceil(userIds.length / MAX_BADGE_USERS_PER_REQUEST) },
        (_, index) =>
          userIds.slice(
            index * MAX_BADGE_USERS_PER_REQUEST,
            (index + 1) * MAX_BADGE_USERS_PER_REQUEST,
          ),
      );
      request = (async () => {
        const badges: KillswitchUserBadge[] = [];
        const unavailableUserIds: string[] = [];
        for (
          let index = 0;
          index < chunks.length;
          index += MAX_CONCURRENT_BADGE_REQUESTS
        ) {
          const wave = chunks.slice(
            index,
            index + MAX_CONCURRENT_BADGE_REQUESTS,
          );
          const results = await Promise.allSettled(
            wave.map((chunk) =>
              Promise.resolve().then(() =>
                batchUserBadges({
                  security: { sessionHeaderGramSession: sessionToken },
                  request: {
                    killswitchBatchUserBadgesRequest: { userIds: chunk },
                  },
                }),
              ),
            ),
          );
          results.forEach((result, resultIndex) => {
            if (result.status === "fulfilled") {
              badges.push(...result.value.badges);
            } else {
              unavailableUserIds.push(...(wave[resultIndex] ?? []));
            }
          });
        }
        return { badges, unavailableUserIds };
      })();
      inFlightBadgeRequests.set(loadKey, request);
      const clearInFlight = () => {
        if (inFlightBadgeRequests.get(loadKey) === request) {
          inFlightBadgeRequests.delete(loadKey);
        }
      };
      void request.then(clearInFlight, clearInFlight);
    }

    void request.then((result) => {
      if (current) onLoaded(result);
    });

    return () => {
      current = false;
    };
  }, [batchUserBadges, loadKey, onLoaded, onLoading, sessionToken, userIds]);

  return null;
}

/** One access-gated, chunked aggregate for the visible users on a surface. */
export function useKillswitchUserBadges(
  userIds: readonly string[],
  enabled = true,
): {
  badges: ReadonlyMap<string, KillswitchUserBadge>;
  unavailableUserIds: ReadonlySet<string>;
  canAccess: boolean;
  isLoading: boolean;
  loader: ReactNode;
} {
  const session = useSession();
  const access = useKillswitchAccess();
  const requestKey = JSON.stringify([...new Set(userIds)].sort());
  const deduplicatedUserIds = useMemo<string[]>(
    () => JSON.parse(requestKey) as string[],
    [requestKey],
  );
  const cacheIdentity = JSON.stringify([
    session.session,
    session.organization.id,
  ]);
  const canLoad = enabled && access.canAccess && deduplicatedUserIds.length > 0;
  const [refreshGeneration, setRefreshGeneration] = useState(0);
  const resultKey = JSON.stringify([cacheIdentity, requestKey]);
  const loadKey = JSON.stringify([resultKey, refreshGeneration]);
  const [result, setResult] = useState<{
    resultKey: string;
    badges: ReadonlyMap<string, KillswitchUserBadge>;
    unavailableUserIds: ReadonlySet<string>;
    isLoading: boolean;
  }>({
    resultKey: "",
    badges: EMPTY_BADGES,
    unavailableUserIds: EMPTY_USER_IDS,
    isLoading: false,
  });

  const current = result.resultKey === resultKey;
  useEffect(() => {
    if (!canLoad || !current || result.isLoading) return;
    const timer = window.setTimeout(
      () => setRefreshGeneration((generation) => generation + 1),
      BADGE_REFRESH_INTERVAL_MS,
    );
    return () => window.clearTimeout(timer);
  }, [canLoad, current, result.isLoading]);

  const onLoading = useCallback(
    () =>
      setResult((previous) =>
        previous.resultKey === resultKey
          ? { ...previous, isLoading: true }
          : {
              resultKey,
              badges: EMPTY_BADGES,
              unavailableUserIds: EMPTY_USER_IDS,
              isLoading: true,
            },
      ),
    [resultKey],
  );
  const onLoaded = useCallback(
    ({ badges, unavailableUserIds }: BadgeLoadResult) =>
      setResult({
        resultKey,
        badges: new Map(badges.map((badge) => [badge.userId, badge])),
        unavailableUserIds: new Set(unavailableUserIds),
        isLoading: false,
      }),
    [resultKey],
  );

  return {
    badges: canLoad && current ? result.badges : EMPTY_BADGES,
    unavailableUserIds:
      canLoad && current ? result.unavailableUserIds : EMPTY_USER_IDS,
    canAccess: enabled && access.canAccess,
    isLoading: canLoad && (!current || result.isLoading),
    loader: canLoad
      ? createElement(KillswitchUserBadgesLoader, {
          key: loadKey,
          loadKey,
          sessionToken: session.session,
          userIds: deduplicatedUserIds,
          onLoading,
          onLoaded,
        })
      : null,
  };
}
