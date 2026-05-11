import type { ChallengeBucket } from "@gram/client/models/components/challengebucket.js";
import { ResolveChallengeFormResolutionType } from "@gram/client/models/components/resolvechallengeform.js";
import { invalidateAllChallenges } from "@gram/client/react-query/challenges.js";
import { invalidateAllChallengeBuckets } from "@gram/client/react-query/challengeBuckets.js";
import { useResolveChallengeMutation } from "@gram/client/react-query/resolveChallenge.js";
import { Button, type Column } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { CreateRoleDialog } from "./CreateRoleDialog";
import { GrantDrawer } from "./GrantDrawer";
import { toRoleSlug } from "./types";

const RESOLVE_LINGER_MS = 3_000;
const FADE_OUT_MS = 1_000;

export function useGrantFlow() {
  const [grantChallenge, setGrantChallenge] = useState<ChallengeBucket | null>(
    null,
  );
  const [isDrawerOpen, setIsDrawerOpen] = useState(false);
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [createChallenge, setCreateChallenge] =
    useState<ChallengeBucket | null>(null);
  const [recentlyResolvedIds, setRecentlyResolvedIds] = useState<Set<string>>(
    () => new Set(),
  );
  const [animatingOutIds, setAnimatingOutIds] = useState<Set<string>>(
    () => new Set(),
  );
  const timersRef = useRef<Map<string, ReturnType<typeof setTimeout>[]>>(
    new Map(),
  );

  const markResolved = useCallback((id: string) => {
    setRecentlyResolvedIds((prev) => new Set(prev).add(id));

    const fadeTimer = setTimeout(() => {
      setAnimatingOutIds((prev) => new Set(prev).add(id));
    }, RESOLVE_LINGER_MS - FADE_OUT_MS);

    const removeTimer = setTimeout(() => {
      setRecentlyResolvedIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
      setAnimatingOutIds((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
      timersRef.current.delete(id);
    }, RESOLVE_LINGER_MS);

    timersRef.current.set(id, [fadeTimer, removeTimer]);
  }, []);

  useEffect(() => {
    const ref = timersRef.current;
    return () => {
      for (const timers of ref.values()) {
        for (const t of timers) clearTimeout(t);
      }
      ref.clear();
    };
  }, []);

  const queryClient = useQueryClient();
  const resolveChallenge = useResolveChallengeMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllChallenges(queryClient),
        invalidateAllChallengeBuckets(queryClient),
      ]);
    },
  });

  const actionsColumn: Column<ChallengeBucket> = useMemo(
    () => ({
      key: "actions",
      header: "",
      width: "100px",
      render: (row: ChallengeBucket) =>
        row.outcome === "deny" && !row.resolvedAt ? (
          <Button
            variant="primary"
            size="sm"
            onClick={() => {
              setGrantChallenge(row);
              setIsDrawerOpen(true);
            }}
          >
            <Button.Text>Grant</Button.Text>
          </Button>
        ) : null,
    }),
    [],
  );

  const challengeIds = grantChallenge?.challengeIds ?? [];

  const grantFlowPortals = (
    <>
      <GrantDrawer
        open={isDrawerOpen}
        onOpenChange={(isOpen) => {
          setIsDrawerOpen(isOpen);
          if (!isOpen) setTimeout(() => setGrantChallenge(null), 350);
        }}
        challenge={grantChallenge}
        challengeIds={challengeIds}
        onResolved={() => {
          if (grantChallenge) markResolved(grantChallenge.id);
        }}
        onCreateNew={() => {
          setCreateChallenge(grantChallenge);
          setIsCreateOpen(true);
        }}
      />

      <CreateRoleDialog
        open={isCreateOpen}
        onOpenChange={(isOpen) => {
          if (!isOpen) {
            setIsCreateOpen(false);
            setCreateChallenge(null);
          }
        }}
        editingRole={null}
        onRoleCreated={(roleName) => {
          if (!createChallenge) return;
          const ids = createChallenge.challengeIds;
          resolveChallenge.mutate(
            {
              request: {
                resolveChallengeForm: {
                  challengeIds: ids,
                  principalUrn: createChallenge.principalUrn,
                  scope: createChallenge.scope,
                  resolutionType:
                    ResolveChallengeFormResolutionType.RoleAssigned,
                  roleSlug: toRoleSlug(roleName),
                  resourceKind: createChallenge.resourceKind,
                  resourceId: createChallenge.resourceId,
                },
              },
            },
            { onSuccess: () => markResolved(createChallenge.id) },
          );
        }}
      />
    </>
  );

  return {
    actionsColumn,
    grantFlowPortals,
    recentlyResolvedIds,
    animatingOutIds,
  };
}
