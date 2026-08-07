import { useMemo, useRef, useState, type ReactNode } from "react";
import { AnimatePresence, motion } from "motion/react";
import { Pencil, Plus } from "lucide-react";
import { match } from "ts-pattern";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import type {
  EmaApp,
  EmaAppAssignment,
  EmaResource,
  EmaTrustRule,
  User,
} from "@/lib/devidp";
import { useUsers } from "@/hooks/use-devidp";
import {
  useEmaApps,
  useEmaAssignments,
  useEmaResources,
  useEmaTrustRules,
} from "@/hooks/use-ema";
import { useEmaLayout } from "@/hooks/use-ema-layout";
import { EmaGraph, type EmaSelection } from "@/components/ema/EmaGraph";
import { AppDialog } from "@/components/ema/AppDialog";
import { ResourceDialog } from "@/components/ema/ResourceDialog";
import { AssignmentDialog } from "@/components/ema/AssignmentDialog";

const CARD_WIDTH = "w-56";

/**
 * The mint-side policy as one picture: which app, acting for which user,
 * reaches which resource. Each route is one assignment row, and the absence
 * of a route is exactly what denies a mint.
 *
 * Trust rules are not routes — they are per-resource, so they ride on the
 * resource cards rather than competing with the routes for the canvas.
 */
export function EmaCanvas() {
  const appsQ = useEmaApps();
  const usersQ = useUsers();
  const resourcesQ = useEmaResources();
  const assignmentsQ = useEmaAssignments();
  const trustQ = useEmaTrustRules();

  const apps = appsQ.data?.items ?? [];
  const users = usersQ.data?.items ?? [];
  const resources = resourcesQ.data?.items ?? [];
  const assignments = assignmentsQ.data?.items ?? [];
  const trustRules = trustQ.data?.items ?? [];

  const [selection, setSelection] = useState<EmaSelection>({ kind: "none" });
  const [creatingApp, setCreatingApp] = useState(false);
  const [creatingResource, setCreatingResource] = useState(false);
  const [assigning, setAssigning] = useState(false);
  const [editingAppID, setEditingAppID] = useState<string | null>(null);
  const [editingResourceID, setEditingResourceID] = useState<string | null>(
    null,
  );
  const [editingAssignmentID, setEditingAssignmentID] = useState<string | null>(
    null,
  );

  const containerRef = useRef<HTMLDivElement>(null);
  const layout = useEmaLayout(containerRef, assignments);

  const isLoading =
    appsQ.isLoading ||
    usersQ.isLoading ||
    resourcesQ.isLoading ||
    assignmentsQ.isLoading;

  const routeOwners = useMemo(
    () =>
      new Map(
        assignments.map((a) => [
          a.id,
          {
            appId: a.app_id,
            userId: a.user_id,
            resourceId: a.resource_id,
          },
        ]),
      ),
    [assignments],
  );

  // Every resource shares this dev-idp's issuer as the obvious thing to
  // trust, and it is derivable from any resource's own issuer URL.
  const localIssuer = useMemo(() => {
    const anyIssuer = resources[0]?.issuer ?? "";
    const cut = anyIssuer.indexOf("/resource-as/");
    return cut === -1 ? "" : `${anyIssuer.slice(0, cut)}/oauth2-1`;
  }, [resources]);

  const trustFor = (resourceID: string) =>
    trustRules.filter((r) => r.resource_id === resourceID);

  const canAssign = apps.length > 0 && users.length > 0 && resources.length > 0;

  return (
    <div className="flex flex-col gap-4">
      <header className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h2 className="text-lg font-medium">Policy</h2>
          <p className="max-w-2xl text-sm text-muted-foreground">
            Each line is one assignment: this app, acting for this user, reaches
            this resource. No line means no ID-JAG — the absence of an
            assignment is the denial. Click a card to trace what it touches, or
            a scope label to change it.
          </p>
        </div>
        <Button onClick={() => setAssigning(true)} disabled={!canAssign}>
          <Plus /> Assign
        </Button>
      </header>

      <div ref={containerRef} className="relative flex justify-between gap-16">
        <EmaGraph
          width={layout.width}
          height={layout.height}
          routes={layout.routes}
          routeOwners={routeOwners}
          selection={selection}
          onSelectRoute={setEditingAssignmentID}
        />

        <Column
          title="Apps"
          empty={!appsQ.isLoading && apps.length === 0}
          emptyLabel="No apps registered."
          onAdd={() => setCreatingApp(true)}
        >
          <AnimatePresence initial={false}>
            {apps.map((app) => (
              <GraphCard
                key={app.id}
                ref={layout.registerApp(app.id)}
                selected={selection.kind === "app" && selection.id === app.id}
                related={isRelated(selection, assignments, "app", app.id)}
                onClick={() => setSelection((s) => toggle(s, "app", app.id))}
                onEdit={() => setEditingAppID(app.id)}
                editLabel="Edit app"
              >
                <AppBody app={app} />
              </GraphCard>
            ))}
          </AnimatePresence>
          {isLoading && <Skeleton />}
        </Column>

        <Column
          title="Users"
          empty={!usersQ.isLoading && users.length === 0}
          emptyLabel="No users — add one on the dev-idp tab."
        >
          <AnimatePresence initial={false}>
            {users.map((user) => (
              <GraphCard
                key={user.id}
                ref={layout.registerUser(user.id)}
                selected={selection.kind === "user" && selection.id === user.id}
                related={isRelated(selection, assignments, "user", user.id)}
                onClick={() => setSelection((s) => toggle(s, "user", user.id))}
              >
                <div className="min-w-0">
                  <div className="truncate font-medium">{user.email}</div>
                  <div className="truncate text-xs text-muted-foreground">
                    {user.display_name}
                  </div>
                </div>
              </GraphCard>
            ))}
          </AnimatePresence>
          {isLoading && <Skeleton />}
        </Column>

        <Column
          title="Resources"
          empty={!resourcesQ.isLoading && resources.length === 0}
          emptyLabel="No resources registered."
          onAdd={() => setCreatingResource(true)}
        >
          <AnimatePresence initial={false}>
            {resources.map((resource) => (
              <GraphCard
                key={resource.id}
                ref={layout.registerResource(resource.id)}
                selected={
                  selection.kind === "resource" && selection.id === resource.id
                }
                related={isRelated(
                  selection,
                  assignments,
                  "resource",
                  resource.id,
                )}
                onClick={() =>
                  setSelection((s) => toggle(s, "resource", resource.id))
                }
                onEdit={() => setEditingResourceID(resource.id)}
                editLabel="Edit resource"
              >
                <ResourceBody
                  resource={resource}
                  trustRules={trustFor(resource.id)}
                />
              </GraphCard>
            ))}
          </AnimatePresence>
          {isLoading && <Skeleton />}
        </Column>
      </div>

      {creatingApp && <AppDialog onClose={() => setCreatingApp(false)} />}
      {creatingResource && (
        <ResourceDialog
          trustRules={trustRules}
          localIssuer={localIssuer}
          onClose={() => setCreatingResource(false)}
        />
      )}
      {assigning && (
        <AssignmentDialog
          apps={apps}
          users={users}
          resources={resources}
          onClose={() => setAssigning(false)}
        />
      )}
      {editingAppID && (
        <AppDialog
          app={apps.find((a) => a.id === editingAppID)}
          onClose={() => setEditingAppID(null)}
        />
      )}
      {editingResourceID && (
        <ResourceDialog
          resource={resources.find((r) => r.id === editingResourceID)}
          trustRules={trustRules}
          localIssuer={localIssuer}
          onClose={() => setEditingResourceID(null)}
        />
      )}
      {editingAssignmentID && (
        <AssignmentDialog
          assignment={assignments.find((a) => a.id === editingAssignmentID)}
          apps={apps}
          users={users}
          resources={resources}
          onClose={() => setEditingAssignmentID(null)}
        />
      )}
    </div>
  );
}

function toggle(
  current: EmaSelection,
  kind: "app" | "user" | "resource",
  id: string,
): EmaSelection {
  return current.kind === kind && current.id === id
    ? { kind: "none" }
    : ({ kind, id } as EmaSelection);
}

/** Whether a card sits on any route touched by the current selection. */
function isRelated(
  selection: EmaSelection,
  assignments: EmaAppAssignment[],
  kind: "app" | "user" | "resource",
  id: string,
): boolean {
  return match(selection)
    .with({ kind: "none" }, () => false)
    .otherwise((s) => {
      if (s.kind === kind && s.id === id) return false;
      const onRoute = (a: EmaAppAssignment, k: string, v: string) =>
        (k === "app" && a.app_id === v) ||
        (k === "user" && a.user_id === v) ||
        (k === "resource" && a.resource_id === v);
      return assignments.some(
        (a) => onRoute(a, s.kind, s.id) && onRoute(a, kind, id),
      );
    });
}

function AppBody({ app }: { app: EmaApp }) {
  // A public app is a legitimate configuration but the weakest of the three,
  // so it is toned to catch the eye when scanning the column.
  //
  // A CIMD client stores no JWKS here — its keys live in its own document —
  // so it would otherwise read as public, which is only true if that document
  // publishes no keys. The card cannot know without fetching, so it names the
  // source rather than guessing the method.
  const method = /^https?:\/\//.test(app.client_id)
    ? { label: "metadata document", tone: "default" as const }
    : app.jwks
      ? { label: "private_key_jwt", tone: "default" as const }
      : app.client_secret
        ? { label: "client_secret_post", tone: "default" as const }
        : { label: "public", tone: "warn" as const };

  return (
    <div className="min-w-0">
      <div className="truncate font-medium">{app.name}</div>
      <div className="truncate font-mono text-xs text-muted-foreground">
        {app.client_id}
      </div>
      <div className="mt-1 flex flex-wrap gap-1">
        <Chip
          tone={method.tone}
          title={
            method.tone === "warn"
              ? "Authenticates with its client_id alone. Minting still needs a valid subject_token and an assignment."
              : method.label === "metadata document"
                ? "Keys are read from this client's own metadata document; a document with none is treated as public."
                : undefined
          }
        >
          {method.label}
        </Chip>
        {!app.enabled && <Chip tone="warn">disabled</Chip>}
      </div>
    </div>
  );
}

function ResourceBody({
  resource,
  trustRules,
}: {
  resource: EmaResource;
  trustRules: EmaTrustRule[];
}) {
  return (
    <div className="min-w-0">
      <div className="truncate font-medium">{resource.name}</div>
      <div className="truncate font-mono text-xs text-muted-foreground">
        {resource.resource_identifier}
      </div>
      <div className="mt-1 flex flex-wrap gap-1">
        {trustRules.length === 0 ? (
          <Chip tone="warn">trusts nothing</Chip>
        ) : (
          trustRules.map((r) => (
            <Chip
              key={r.id}
              tone={r.enabled ? "default" : "warn"}
              title={`trusts ${r.trusted_issuer}${
                r.allowed_scopes ? ` — ceiling ${r.allowed_scopes}` : ""
              }${r.enabled ? "" : " (disabled)"}`}
            >
              {shortIssuer(r.trusted_issuer)}
              {r.allowed_scopes ? ` ≤ ${r.allowed_scopes}` : ""}
            </Chip>
          ))
        )}
      </div>
    </div>
  );
}

/** Issuer URLs are long and mostly host; the path is what distinguishes them. */
function shortIssuer(issuer: string): string {
  try {
    const u = new URL(issuer);
    return `${u.host}${u.pathname}`.replace(/\/$/, "");
  } catch {
    return issuer;
  }
}

function Chip({
  children,
  tone = "default",
  title,
}: {
  children: ReactNode;
  tone?: "default" | "warn";
  title?: string;
}) {
  return (
    <span
      title={title}
      className={cn(
        // Issuer URLs are long and the card is narrow, so a chip clips rather
        // than spilling past the card edge. The full value is in the title.
        "max-w-full truncate rounded-sm border px-1 py-[1px] font-mono text-[10px]",
        tone === "warn"
          ? "border-[var(--retro-orange)]/40 text-[var(--retro-orange)]"
          : "border-border text-muted-foreground",
      )}
    >
      {children}
    </span>
  );
}

function Column({
  title,
  empty,
  emptyLabel,
  onAdd,
  children,
}: {
  title: string;
  empty: boolean;
  emptyLabel: string;
  onAdd?: () => void;
  children: ReactNode;
}) {
  return (
    <section
      className={cn("relative z-10 flex shrink-0 flex-col gap-3", CARD_WIDTH)}
    >
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium uppercase tracking-wide text-muted-foreground">
          {title}
        </h3>
        {onAdd && (
          <Button variant="ghost" size="xs" onClick={onAdd}>
            <Plus /> Add
          </Button>
        )}
      </div>
      <div className="flex flex-col gap-3">
        {children}
        {empty && (
          <div className="text-sm italic text-muted-foreground">
            {emptyLabel}
          </div>
        )}
      </div>
    </section>
  );
}

function GraphCard({
  ref,
  selected,
  related,
  onClick,
  onEdit,
  editLabel,
  children,
}: {
  ref: (el: HTMLElement | null) => void;
  selected: boolean;
  related: boolean;
  onClick: () => void;
  onEdit?: () => void;
  editLabel?: string;
  children: ReactNode;
}) {
  return (
    <motion.div
      ref={ref as React.Ref<HTMLDivElement>}
      layout
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
        <CardContent>
          <div className="flex items-start justify-between gap-2">
            {children}
            {onEdit && (
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                onClick={(e) => {
                  e.stopPropagation();
                  onEdit();
                }}
                aria-label={editLabel}
              >
                <Pencil />
              </Button>
            )}
          </div>
        </CardContent>
      </Card>
    </motion.div>
  );
}

function Skeleton() {
  return (
    <div className={cn("h-16 animate-pulse rounded-md bg-muted", CARD_WIDTH)} />
  );
}
