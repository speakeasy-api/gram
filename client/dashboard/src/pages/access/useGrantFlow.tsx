import { ResolveChallengeFormResolutionType } from "@gram/client/models/components/resolvechallengeform.js";
import { invalidateAllChallenges } from "@gram/client/react-query/challenges.js";
import { useResolveChallengeMutation } from "@gram/client/react-query/resolveChallenge.js";
import { Button, type Column } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { AuthzChallenge } from "./ChallengesTab";
import { CreateRoleDialog } from "./CreateRoleDialog";
import { GrantDrawer } from "./GrantDrawer";
import { toRoleSlug } from "./types";

const RESOLVE_LINGER_MS = 3_000;
const FADE_OUT_MS = 1_000;

export function useGrantFlow(
  getGroupChallengeIds?: (challengeId: string) => string[],
) {
  const [grantChallenge, setGrantChallenge] = useState<AuthzChallenge | null>(
    null,
  );
  const [isDrawerOpen, setIsDrawerOpen] = useState(false);
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [createChallenge, setCreateChallenge] = useState<AuthzChallenge | null>(
    null,
  );
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

  const markManyResolved = useCallback(
    (ids: string[]) => {
      for (const id of ids) markResolved(id);
    },
    [markResolved],
  );

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
      await invalidateAllChallenges(queryClient);
    },
  });

  const actionsColumn: Column<AuthzChallenge> = useMemo(
    () => ({
      key: "actions",
      header: "",
      width: "100px",
      render: (row: AuthzChallenge) =>
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

  const challengeIds = grantChallenge
    ? (getGroupChallengeIds?.(grantChallenge.id) ?? [grantChallenge.id])
    : [];

  const grantFlowPortals = (
    <>
      <GrantDrawer
        open={isDrawerOpen}
        onOpenChange={(isOpen) => {
          setIsDrawerOpen(isOpen);
          // Delay clearing challenge so Sheet exit animation can complete
          if (!isOpen) setTimeout(() => setGrantChallenge(null), 350);
        }}
        challenge={grantChallenge}
        challengeIds={challengeIds}
        onResolved={markManyResolved}
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
          const ids = getGroupChallengeIds?.(createChallenge.id) ?? [
            createChallenge.id,
          ];
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
            { onSuccess: () => markManyResolved(ids) },
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
