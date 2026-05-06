import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Building2, Pencil, Plus } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { useMemberships, useOrganizations, useUsers } from "@/hooks/use-devidp";
import type { Mode, User, WorkosCurrentUser } from "@/lib/devidp";
import { EditOrgModal } from "@/components/EditOrgModal";
import { EditUserModal } from "@/components/EditUserModal";

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
 * Resolves the local user-id to look up memberships against, regardless of
 * mode. local-speakeasy hands us the id directly; workos requires a bounce
 * through the shadow lookup.
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
  const { userId, loading: idLoading } = useEffectiveUserId(props);
  const orgsQ = useOrganizations();
  const usersQ = useUsers();
  const membershipsQ = useMemberships();
  const [editingOrgId, setEditingOrgId] = useState<string | null>(null);
  const [managingMemberships, setManagingMemberships] = useState(false);

  const allOrgs = orgsQ.data?.items ?? [];
  const allUsers = usersQ.data?.items ?? [];
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
  const localUser = userId
    ? (allUsers.find((u) => u.id === userId) ?? null)
    : null;

  const isLoading =
    idLoading || orgsQ.isLoading || membershipsQ.isLoading || usersQ.isLoading;

  const editingOrg = editingOrgId ? orgsById.get(editingOrgId) : null;

  return (
    <>
      <Card>
        <CardHeader className="border-b pb-4">
          <CardTitle className="flex items-center gap-2 text-sm font-semibold">
            <Building2 className="size-4 text-muted-foreground" />
            Organizations
            {userMemberships.length > 0 && (
              <span className="text-[10px] font-mono text-muted-foreground rounded-full border border-border px-1.5">
                {userMemberships.length}
              </span>
            )}
          </CardTitle>
          {localUser && (
            <CardAction>
              <Button
                type="button"
                variant="outline"
                size="xs"
                onClick={() => setManagingMemberships(true)}
              >
                <Plus />
                Manage memberships
              </Button>
            </CardAction>
          )}
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
                    "h-12 rounded-md bg-muted animate-pulse",
                    i === 1 && "opacity-60",
                  )}
                />
              ))}
            </div>
          ) : userMemberships.length === 0 ? (
            <EmptyMemberships
              onAdd={localUser ? () => setManagingMemberships(true) : null}
            />
          ) : (
            <ul className="divide-y divide-border -my-3">
              {userMemberships.map((m) => {
                const org = orgsById.get(m.organization_id);
                return (
                  <li
                    key={m.id}
                    className="py-3 flex items-center gap-3 group"
                  >
                    <Building2 className="size-4 text-muted-foreground shrink-0" />
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
                    <RoleBadge role={m.role} />
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
      {managingMemberships && localUser && (
        <EditUserModal
          user={localUser}
          layoutId={`dash-user-${localUser.id}`}
          onClose={() => setManagingMemberships(false)}
        />
      )}
    </>
  );
}

function RoleBadge({ role }: { role: string }) {
  const isAdmin = role.toLowerCase() === "admin";
  return (
    <span
      className={cn(
        "text-[10px] font-mono uppercase tracking-wider rounded-sm border px-1.5 py-0.5",
        isAdmin
          ? "bg-[var(--retro-yellow)]/20 border-[var(--retro-yellow)]/40 text-foreground"
          : "bg-muted/40 border-border text-muted-foreground",
      )}
    >
      {role}
    </span>
  );
}

function EmptyMemberships({ onAdd }: { onAdd: (() => void) | null }) {
  return (
    <div className="rounded-md border border-dashed border-border bg-muted/20 px-4 py-6 text-center space-y-3">
      <div className="text-sm text-muted-foreground">
        This user isn't a member of any organisation yet.
      </div>
      {onAdd && (
        <Button type="button" variant="outline" size="sm" onClick={onAdd}>
          <Plus />
          Join an organisation
        </Button>
      )}
    </div>
  );
}
