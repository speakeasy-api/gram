import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Building2, Pencil } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { useMemberships, useOrganizations } from "@/hooks/use-devidp";
import type { Mode, User, WorkosCurrentUser } from "@/lib/devidp";
import { EditOrgModal } from "@/components/EditOrgModal";

interface WorkosShadow {
  workos_sub: string;
  shadow_id?: string;
}

async function fetchWorkos(suffix: string): Promise<unknown> {
  const res = await fetch(`/devidp/workos/${suffix}`);
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
  return res.json();
}

interface Props {
  mode: Mode;
  user: User | null;
  workos: WorkosCurrentUser | null;
}

/**
 * Resolves the local user-id we should look up memberships for, regardless
 * of which mode is active. local-speakeasy returns the user id directly;
 * workos has to bounce through the shadow lookup.
 */
function useEffectiveUserId(props: Props): {
  userId: string | null;
  loading: boolean;
} {
  const { mode, user, workos } = props;
  const sub = workos?.workos_sub;
  const shadowQ = useQuery<WorkosShadow>({
    queryKey: ["workos", "shadow", sub],
    queryFn: () => fetchWorkos("currentUser") as Promise<WorkosShadow>,
    enabled: mode === "workos" && !!sub,
  });

  if (mode === "workos") {
    return {
      userId: shadowQ.data?.shadow_id ?? null,
      loading: shadowQ.isLoading,
    };
  }
  return { userId: user?.id ?? null, loading: false };
}

export function OrganizationsCard(props: Props) {
  const { mode } = props;

  if (mode === "oauth2" || mode === "oauth2-1") return null;

  return <OrganizationsCardInner {...props} />;
}

function OrganizationsCardInner(props: Props) {
  const { userId, loading } = useEffectiveUserId(props);
  const orgsQ = useOrganizations();
  const membershipsQ = useMemberships();
  const [editingOrgId, setEditingOrgId] = useState<string | null>(null);

  const allOrgs = orgsQ.data?.items ?? [];
  const allMemberships = membershipsQ.data?.items ?? [];
  const orgsById = useMemo(
    () => new Map(allOrgs.map((o) => [o.id, o])),
    [allOrgs],
  );
  const userMemberships = useMemo(
    () =>
      userId ? allMemberships.filter((m) => m.user_id === userId) : [],
    [allMemberships, userId],
  );

  const isLoading =
    loading || orgsQ.isLoading || membershipsQ.isLoading;

  const editingOrg = editingOrgId ? orgsById.get(editingOrgId) : null;

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Building2 className="size-4 text-muted-foreground" />
            Organizations
            {userMemberships.length > 0 && (
              <span className="text-xs font-normal text-muted-foreground">
                ({userMemberships.length})
              </span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {!userId ? (
            <div className="text-sm text-muted-foreground italic">
              Pick a current user above to see their organizations.
            </div>
          ) : isLoading ? (
            <div className="space-y-2">
              {[0, 1].map((i) => (
                <div
                  key={i}
                  className={cn(
                    "h-14 rounded-md bg-muted animate-pulse",
                    i === 1 && "opacity-60",
                  )}
                />
              ))}
            </div>
          ) : userMemberships.length === 0 ? (
            <div className="text-sm text-muted-foreground italic">
              This user is not a member of any organisation yet. Edit the user
              from the legacy graph view to add memberships.
            </div>
          ) : (
            <ul className="divide-y divide-border -my-2">
              {userMemberships.map((m) => {
                const org = orgsById.get(m.organization_id);
                return (
                  <li
                    key={m.id}
                    className="py-3 flex items-center gap-3 group"
                  >
                    <div className="min-w-0 flex-1">
                      <div className="font-medium truncate">
                        {org?.name ?? m.organization_id}
                      </div>
                      <div className="text-xs text-muted-foreground truncate font-mono">
                        {org?.slug ?? "—"}
                        {org?.account_type && (
                          <>
                            {" · "}
                            <span className="uppercase tracking-wider">
                              {org.account_type}
                            </span>
                          </>
                        )}
                      </div>
                    </div>
                    <span className="text-[10px] font-mono uppercase tracking-wider text-muted-foreground rounded-sm border border-border bg-muted/40 px-1.5 py-0.5">
                      {m.role}
                    </span>
                    {org && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-xs"
                        onClick={() => setEditingOrgId(org.id)}
                        aria-label={`Edit ${org.name}`}
                      >
                        <Pencil />
                      </Button>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </CardContent>
      </Card>

      {editingOrg && (
        <EditOrgModal
          org={editingOrg}
          layoutId={`dash-org-${editingOrg.id}`}
          onClose={() => setEditingOrgId(null)}
        />
      )}
    </>
  );
}
