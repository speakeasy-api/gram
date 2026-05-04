import { useMemo, useRef, useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { match } from "ts-pattern";
import { Pencil, Plus } from "lucide-react";
import { cn } from "@/lib/utils";
import { useMemberships, useOrganizations, useUsers } from "@/hooks/use-devidp";
import { useMembershipLayout } from "@/hooks/use-membership-layout";
import type { Membership, Organization, User } from "@/lib/devidp";
import { MembershipGraph } from "@/components/MembershipGraph";
import { CreateOrgDialog } from "@/components/CreateOrgDialog";
import { CreateUserDialog } from "@/components/CreateUserDialog";
import { EditOrgModal } from "@/components/EditOrgModal";
import { EditUserModal } from "@/components/EditUserModal";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";

const orgLayoutId = (id: string) => `card-org-${id}`;
const userLayoutId = (id: string) => `card-user-${id}`;

type Selection =
  | { kind: "none" }
  | { kind: "org"; id: string }
  | { kind: "user"; id: string };

export function HomeTab() {
  const orgsQ = useOrganizations();
  const usersQ = useUsers();
  const membershipsQ = useMemberships();

  const [selection, setSelection] = useState<Selection>({ kind: "none" });
  const [creatingOrg, setCreatingOrg] = useState(false);
  const [creatingUser, setCreatingUser] = useState(false);
  const [editingOrgId, setEditingOrgId] = useState<string | null>(null);
  const [editingUserId, setEditingUserId] = useState<string | null>(null);

  const orgs = orgsQ.data?.items ?? [];
  const users = usersQ.data?.items ?? [];
  const memberships = membershipsQ.data?.items ?? [];

  const containerRef = useRef<HTMLDivElement>(null);
  const layout = useMembershipLayout(containerRef, memberships);

  const isLoading =
    orgsQ.isLoading || usersQ.isLoading || membershipsQ.isLoading;

  const edgeOwners = useMemo(
    () =>
      new Map(
        memberships.map((m) => [
          m.id,
          { orgId: m.organization_id, userId: m.user_id },
        ]),
      ),
    [memberships],
  );

  return (
    <div className="grid grid-rows-[auto_1fr] gap-4 max-w-6xl mx-auto">
      <p className="text-sm text-muted-foreground">
        Click a card to highlight its memberships. Lines animate between
        organizations and users; each pill marks the role.
      </p>

      <div ref={containerRef} className="relative flex justify-between gap-16">
        <MembershipGraph
          width={layout.width}
          height={layout.height}
          edges={layout.edges}
          edgeOwners={edgeOwners}
          selection={selection}
        />

        <Column
          title="Organizations"
          empty={!orgsQ.isLoading && orgs.length === 0}
          onAdd={() => setCreatingOrg(true)}
        >
          <AnimatePresence initial={false}>
            {orgs.map((org) => (
              <OrgCard
                key={org.id}
                org={org}
                selected={selection.kind === "org" && selection.id === org.id}
                related={isRelated(selection, memberships, "org", org.id)}
                onSelect={() =>
                  setSelection((s) =>
                    s.kind === "org" && s.id === org.id
                      ? { kind: "none" }
                      : { kind: "org", id: org.id },
                  )
                }
                onEdit={() => setEditingOrgId(org.id)}
                refSetter={layout.registerOrg(org.id)}
              />
            ))}
          </AnimatePresence>
          {isLoading && <Skeleton />}
        </Column>

        <Column
          title="Users"
          empty={!usersQ.isLoading && users.length === 0}
          onAdd={() => setCreatingUser(true)}
        >
          <AnimatePresence initial={false}>
            {users.map((user) => (
              <UserCard
                key={user.id}
                user={user}
                selected={selection.kind === "user" && selection.id === user.id}
                related={isRelated(selection, memberships, "user", user.id)}
                onSelect={() =>
                  setSelection((s) =>
                    s.kind === "user" && s.id === user.id
                      ? { kind: "none" }
                      : { kind: "user", id: user.id },
                  )
                }
                onEdit={() => setEditingUserId(user.id)}
                refSetter={layout.registerUser(user.id)}
              />
            ))}
          </AnimatePresence>
          {isLoading && <Skeleton />}
        </Column>
      </div>

      {creatingOrg && <CreateOrgDialog onClose={() => setCreatingOrg(false)} />}
      {creatingUser && (
        <CreateUserDialog
          users={users}
          orgs={orgs}
          onClose={() => setCreatingUser(false)}
        />
      )}
      {editingOrgId &&
        (() => {
          const org = orgs.find((o) => o.id === editingOrgId);
          if (!org) return null;
          return (
            <EditOrgModal
              org={org}
              layoutId={orgLayoutId(org.id)}
              onClose={() => setEditingOrgId(null)}
            />
          );
        })()}
      {editingUserId &&
        (() => {
          const user = users.find((u) => u.id === editingUserId);
          if (!user) return null;
          return (
            <EditUserModal
              user={user}
              layoutId={userLayoutId(user.id)}
              onClose={() => setEditingUserId(null)}
            />
          );
        })()}
    </div>
  );
}

function isRelated(
  selection: Selection,
  memberships: Membership[],
  side: "org" | "user",
  id: string,
): boolean {
  return match(selection)
    .with({ kind: "none" }, () => false)
    .with({ kind: "org" }, (s) =>
      side === "user"
        ? memberships.some(
            (m) => m.organization_id === s.id && m.user_id === id,
          )
        : false,
    )
    .with({ kind: "user" }, (s) =>
      side === "org"
        ? memberships.some(
            (m) => m.user_id === s.id && m.organization_id === id,
          )
        : false,
    )
    .exhaustive();
}

function Column({
  title,
  empty,
  onAdd,
  children,
}: {
  title: string;
  empty: boolean;
  onAdd: () => void;
  children: React.ReactNode;
}) {
  return (
    <section
      className={cn("relative z-10 flex flex-col gap-3 shrink-0", CARD_WIDTH)}
    >
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wide">
          {title}
        </h2>
        <Button variant="ghost" size="xs" onClick={onAdd}>
          <Plus /> Add
        </Button>
      </div>
      <div className="flex flex-col gap-3">
        {children}
        {empty && (
          <div className="text-sm text-muted-foreground italic">None yet.</div>
        )}
      </div>
    </section>
  );
}

const CARD_WIDTH = "w-56";

function MembershipCard({
  ref,
  layoutId,
  selected,
  related,
  onClick,
  children,
}: {
  ref: (el: HTMLElement | null) => void;
  layoutId: string;
  selected: boolean;
  related: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <motion.div
      ref={ref as React.Ref<HTMLDivElement>}
      layout
      layoutId={layoutId}
      onClick={onClick}
      whileHover={{ scale: 1.005 }}
      whileTap={{ scale: 0.995 }}
      transition={{ type: "spring", stiffness: 500, damping: 35 }}
      className={cn(
        "cursor-pointer rounded-md",
        CARD_WIDTH,
        selected && "gradient-outline",
      )}
    >
      <Card
        size="sm"
        className={cn(
          "!rounded-md transition-all",
          related && !selected && "ring-1 ring-[var(--retro-yellow)]/50",
        )}
      >
        <CardContent>{children}</CardContent>
      </Card>
    </motion.div>
  );
}

function OrgCard({
  org,
  selected,
  related,
  onSelect,
  onEdit,
  refSetter,
}: {
  org: Organization;
  selected: boolean;
  related: boolean;
  onSelect: () => void;
  onEdit: () => void;
  refSetter: (el: HTMLElement | null) => void;
}) {
  return (
    <MembershipCard
      ref={refSetter}
      layoutId={orgLayoutId(org.id)}
      selected={selected}
      related={related}
      onClick={onSelect}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="font-medium truncate">{org.name}</div>
          <div className="text-xs text-muted-foreground truncate">
            {org.slug} · {org.account_type}
          </div>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={(e) => {
            e.stopPropagation();
            onEdit();
          }}
          aria-label="Edit organization"
        >
          <Pencil />
        </Button>
      </div>
    </MembershipCard>
  );
}

function UserCard({
  user,
  selected,
  related,
  onSelect,
  onEdit,
  refSetter,
}: {
  user: User;
  selected: boolean;
  related: boolean;
  onSelect: () => void;
  onEdit: () => void;
  refSetter: (el: HTMLElement | null) => void;
}) {
  return (
    <MembershipCard
      ref={refSetter}
      layoutId={userLayoutId(user.id)}
      selected={selected}
      related={related}
      onClick={onSelect}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="font-medium truncate">{user.display_name}</div>
          <div className="text-xs text-muted-foreground truncate">
            {user.email}
          </div>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          onClick={(e) => {
            e.stopPropagation();
            onEdit();
          }}
          aria-label="Edit user"
        >
          <Pencil />
        </Button>
      </div>
    </MembershipCard>
  );
}

function Skeleton() {
  return (
    <div className="space-y-2">
      {[0, 1, 2].map((i) => (
        <div
          key={i}
          className={cn(
            "h-16 rounded-md bg-muted animate-pulse",
            CARD_WIDTH,
            i === 1 && "opacity-70",
            i === 2 && "opacity-50",
          )}
        />
      ))}
    </div>
  );
}
