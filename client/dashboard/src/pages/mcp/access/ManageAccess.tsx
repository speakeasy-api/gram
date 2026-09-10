import {
  InlineChoice,
  type InlineChoiceOption,
} from "@/components/access/AccessListRow";
import { IdentityLink } from "@/components/identity-link";
import {
  MemberFacepile,
  type FacepileMember,
} from "@/components/member-facepile";
import { RequireScope } from "@/components/require-scope";
import { useRBAC } from "@/hooks/useRBAC";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import { Link, useNavigate } from "react-router";
import type { SetResourceAudienceEntry } from "@gram/client/models/components/setresourceaudienceentry.js";
import type { ResourceAudienceEntry } from "@gram/client/models/components/resourceaudienceentry.js";
import { invalidateAllResourceAudience } from "@gram/client/react-query/resourceAudience.js";
import { useSetResourceAudienceMutation } from "@gram/client/react-query/setResourceAudience.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useQueryClient } from "@tanstack/react-query";
import {
  ChevronLeft,
  ChevronRight,
  Pencil,
  PencilOff,
  Plus,
  Trash2,
} from "lucide-react";
import { cn } from "@/lib/utils";
import {
  useMemo,
  useState,
  type ComponentProps,
  type JSX,
  type ReactNode,
} from "react";
import { toast } from "sonner";
import { AddAudienceDialog } from "./AddAudienceDialog";
import { RemoveAudienceDialog } from "./RemoveAudienceDialog";
import type { ToolSelectionTool } from "@/components/tool-selection/ToolSelectionPanel";
import { ToolNarrowingDialog } from "./ToolNarrowingDialog";
import { pageCount, pageOf } from "./manageAccessState";
import {
  accessSummary,
  buildAccessRows,
  reachableTools,
  inheritedGrants,
  scopeState,
  SCOPE_ROWS,
  type AccessRow,
  type ScopeKey,
} from "./accessRows";
import {
  addPrincipalsWrite,
  allowWrite,
  narrowingSeed,
  narrowWrite,
  revokeRowWrite,
  revokeScopeWrite,
  type AudienceWrite,
} from "./accessWrites";
import { ownRules, LEVEL_VERB } from "./serverAudience";

/** Narrowing the tool dialog is currently editing, and the row it belongs to. */
interface NarrowingTarget {
  row: AccessRow;
  tools: string[];
  dispositions: string[];
  /** The tools this row could reach, when something caps it below the whole
   * catalogue. Offering the rest would let an administrator pick tools the
   * server will refuse anyway. */
  catalog?: ToolSelectionTool[];
  /** The principal whose block caps it, for the dialog to name. */
  limitedBy?: string;
}

/**
 * Manage access for one resource. One list, one row per principal — a person,
 * everyone, or a role — and inside each row a line for every scope the server
 * has: connect, view and manage. Rules naming this resource are written here;
 * an organization-wide rule is narrowed by subtracting from it, which is the
 * only per-server edit that leaves every other server alone.
 */
export function ManageAccess({
  resourceId,
  resourceName,
  entries,
  version,
  toolCatalog,
  isLoading,
}: {
  resourceId: string;
  resourceName?: string;
  entries: ResourceAudienceEntry[];
  /** Fingerprint of the rules this list was read from. */
  version: string;
  /** The server's tools, when this backend exposes a catalogue. */
  toolCatalog?: ToolSelectionTool[];
  isLoading: boolean;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const navigate = useNavigate();

  // The row controls are gated by RequireScope; the remove button carries the
  // same gate as a disabled state so it cannot be clicked without the scope.
  const { hasAnyScope } = useRBAC();
  const canManage = hasAnyScope(["org:admin"]);
  const [page, setPage] = useState(0);
  const [adding, setAdding] = useState(false);
  const [narrowing, setNarrowing] = useState<NarrowingTarget | null>(null);
  // The row whose removal is waiting to be confirmed, when the write is not
  // the plain deletion the button looks like.
  const [removing, setRemoving] = useState<AccessRow | null>(null);

  // The organization's members, so a role row can show who it reaches. The
  // audience entries carry ids; the names and photos live here.
  const { data: membersData } = useMembers();
  const facesById = useMemo(() => {
    const members = membersData?.members ?? [];
    return new Map(
      members.map((member) => [
        member.id,
        {
          id: member.id,
          name: member.name,
          email: member.email,
          photoUrl: member.photoUrl,
        },
      ]),
    );
  }, [membersData]);

  const direct = useMemo(() => ownRules(entries), [entries]);
  const rows = useMemo(() => buildAccessRows(entries), [entries]);
  // People a block already reaches, named by the block itself rather than
  // read off the rows: someone with no rule of their own here has no row,
  // and granting them access would write a rule the block cancels.
  const cancelled = useMemo(() => {
    const byUser = new Map<string, string>();
    for (const entry of entries) {
      if (entry.level !== "blocked") continue;
      if ((entry.tools ?? []).length || (entry.dispositions ?? []).length) {
        continue;
      }
      for (const memberId of entry.memberIds ?? []) {
        if (!byUser.has(memberId)) byUser.set(memberId, entry.displayName);
      }
    }
    return byUser;
  }, [entries]);

  // Rules covering every server, which a rule added here cannot narrow.
  const orgWide = useMemo(
    () => entries.filter((entry) => entry.appliesTo !== "resource"),
    [entries],
  );

  const visible = pageOf(rows, page);
  const pages = pageCount(rows.length);

  const setAudience = useSetResourceAudienceMutation({
    onSuccess: async () => {
      await invalidateAllResourceAudience(queryClient);
      setAdding(false);
    },
    // The server refuses some writes for a reason worth reading — blocking
    // your own access, or a list that changed underneath this one — so its
    // message is shown rather than a generic failure.
    onError: (error) =>
      toast.error(
        error instanceof Error && error.message
          ? error.message
          : "Could not save access. Please try again.",
      ),
  });

  const save = (next: SetResourceAudienceEntry[], message: string) => {
    setAudience.mutate(
      {
        request: {
          setResourceAudienceForm: {
            resourceKind: "mcp",
            resourceId,
            entries: next,
            // The list this edit was based on. A save against a stale one is
            // refused rather than quietly replacing someone else's work.
            expectedVersion: version,
          },
        },
      },
      {
        onSuccess: () => {
          toast.success(message);
        },
      },
    );
  };

  const applyWrite = (write: AudienceWrite) =>
    save(write.entries, write.message);

  const changeNarrowing = (
    target: NarrowingTarget,
    next: { tools: string[]; dispositions: string[] },
  ) => {
    setNarrowing(null);
    applyWrite(
      narrowWrite(direct, target.row, next, toolCatalog, resourceName),
    );
  };

  const openNarrowing = (row: AccessRow) => {
    // A block this row cannot lift caps what it can reach, so the picker
    // leaves those tools out rather than accepting a choice that does
    // nothing — and it opens on what the row reaches today, so saving
    // without touching anything cannot widen access.
    const cell = row.cells.use;
    const capped = scopeState(row, "use", toolCatalog ?? []).capped;
    const reachable = capped
      ? new Set(reachableTools(cell, toolCatalog ?? []) ?? [])
      : null;
    const limitedBy = capped
      ? cell.blocks.find(
          (block) =>
            block.principalUrn !== row.principalUrn ||
            block.appliesTo !== "resource",
        )?.displayName
      : undefined;

    setNarrowing({
      row,
      ...narrowingSeed(row, toolCatalog),
      ...(reachable
        ? {
            catalog: (toolCatalog ?? []).filter((tool) =>
              reachable.has(tool.name),
            ),
            limitedBy,
          }
        : {}),
    });
  };

  // An organization-wide rule belongs to the role that holds it.
  const editRole = (row: AccessRow) => {
    const roleId = row.principalUrn.split(":").pop();
    if (!roleId) return;
    void navigate(`${orgRoutes.access.roles.href()}/${roleId}/edit`);
  };

  // Removing a rule this page owns is what the button says it is. Removing a
  // principal an organization-wide rule still reaches is not: it writes a
  // block, which outranks every grant. Only that case is worth a dialog.
  const removeRow = (row: AccessRow) => {
    if (inheritedGrants(row).length === 0) {
      applyWrite(revokeRowWrite(direct, row, resourceName));
      return;
    }
    setRemoving(row);
  };

  const confirmRemove = () => {
    if (!removing) return;
    applyWrite(revokeRowWrite(direct, removing, resourceName));
    setRemoving(null);
  };

  const addPrincipals = (principalUrns: string[]) => {
    setPage(0);
    applyWrite(addPrincipalsWrite(direct, principalUrns, resourceName));
  };

  if (isLoading) return <SkeletonTable />;

  return (
    <div>
      <Card.Dashboard
        title="Manage access to this server"
        // Rows run edge to edge under the header, like the dashboard's other
        // list cards, and the title bar sits tight above them.
        bodyClassName="p-0"
        headerClassName="py-2.5"
        action={
          <RequireScope
            scope="org:admin"
            level="component"
            reason="Only organization admins can change access."
          >
            <Button
              variant="secondary"
              size="sm"
              onClick={() => setAdding(true)}
            >
              <Button.LeftIcon>
                <Plus className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Grant access</Button.Text>
            </Button>
          </RequireScope>
        }
      >
        {visible.length === 0 ? (
          <div className="px-4 py-12 text-center">
            <Text muted small>
              Nobody reaches {resourceName ?? "this server"} yet.
            </Text>
          </div>
        ) : (
          <div className="grid grid-cols-[minmax(0,18rem)_minmax(0,1fr)_auto_auto]">
            {visible.map((row) => (
              <PrincipalRow
                key={row.principalUrn}
                row={row}
                faces={row.memberIds
                  .map((id) => facesById.get(id))
                  .filter((member) => member !== undefined)}
                catalog={toolCatalog ?? []}
                hasCatalog={Boolean(toolCatalog)}
                onAllow={(scope) =>
                  applyWrite(allowWrite(direct, row, scope, resourceName))
                }
                onRevokeScope={(scope) =>
                  applyWrite(revokeScopeWrite(direct, row, scope, resourceName))
                }
                onNarrow={() => openNarrowing(row)}
                onRevoke={() => removeRow(row)}
                onEditRole={() => editRole(row)}
                canManage={canManage}
                pending={setAudience.isPending}
              />
            ))}
          </div>
        )}
      </Card.Dashboard>

      {pages > 1 && (
        <div className="mt-4 flex items-center justify-center gap-4">
          <Button
            variant="tertiary"
            size="sm"
            disabled={page === 0}
            onClick={() => setPage((p) => Math.max(0, p - 1))}
          >
            <Button.LeftIcon>
              <ChevronLeft className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Previous</Button.Text>
          </Button>
          <Text muted small>
            {page + 1} of {pages}
          </Text>
          <Button
            variant="tertiary"
            size="sm"
            disabled={page >= pages - 1}
            onClick={() => setPage((p) => Math.min(pages - 1, p + 1))}
          >
            <Button.Text>Next</Button.Text>
            <Button.RightIcon>
              <ChevronRight className="h-4 w-4" />
            </Button.RightIcon>
          </Button>
        </div>
      )}

      {narrowing && (
        <ToolNarrowingDialog
          serverId={resourceId}
          serverName={resourceName}
          catalog={narrowing.catalog ?? toolCatalog}
          limitedBy={narrowing.limitedBy}
          tools={narrowing.tools}
          dispositions={narrowing.dispositions}
          pending={setAudience.isPending}
          onSave={(next) => changeNarrowing(narrowing, next)}
          onClose={() => setNarrowing(null)}
        />
      )}

      {removing && (
        <RemoveAudienceDialog
          row={removing}
          serverName={resourceName}
          pending={setAudience.isPending}
          onConfirm={confirmRemove}
          onClose={() => setRemoving(null)}
        />
      )}

      {adding && (
        <AddAudienceDialog
          title="Grant access"
          description={`Give people access to ${resourceName ?? "this server"} only. To give a role access, edit the role.`}
          kinds={["user"]}
          alreadyAdded={direct.map((entry) => entry.principalUrn)}
          // A principal an organization-wide rule already covers cannot be
          // narrowed by adding a rule here — grants add, they never subtract
          // — so say what it already has instead of offering a no-op.
          blockedFrom={[...cancelled].map(([userId, by]) => ({
            principalUrn: `user:${userId}`,
            reason: `Blocked by ${by} on this server`,
          }))}
          alreadyReaches={orgWide
            // A narrowed organization rule leaves room to grant more here, so
            // only unrestricted ones make a principal unaddable.
            .filter(
              (entry) =>
                (entry.tools ?? []).length === 0 &&
                (entry.dispositions ?? []).length === 0,
            )
            .map((entry) => ({
              principalUrn: entry.principalUrn,
              reason: `Already can ${LEVEL_VERB[entry.level]} on every server`,
            }))}
          pending={setAudience.isPending}
          onAdd={addPrincipals}
          onClose={() => setAdding(false)}
        />
      )}
    </div>
  );
}

/**
 * One principal, as a row in the list's shared column grid: what it is, what
 * it can do here, who it reaches, and the controls. Columns line up down the
 * list, so an administrator compares rows by reading straight down rather
 * than re-parsing a sentence each time.
 */
function PrincipalRow({
  row,
  faces,
  catalog,
  hasCatalog,
  onAllow,
  onRevokeScope,
  onNarrow,
  onRevoke,
  onEditRole,
  canManage,
  pending,
}: {
  row: AccessRow;
  /** The people a role reaches, for the facepile on its row. */
  faces: FacepileMember[];
  /** The server's tools, for resolving what a rule and a block leave. */
  catalog: ToolSelectionTool[];
  hasCatalog: boolean;
  onAllow: (scope: ScopeKey) => void;
  onRevokeScope: (scope: ScopeKey) => void;
  onNarrow: () => void;
  onRevoke: () => void;
  onEditRole: () => void;
  canManage: boolean;
  pending: boolean;
}): JSX.Element {
  // Collapsed by default: an administrator scanning the list wants to know
  // who reaches the server, and only opens the row they came to change.
  const [open, setOpen] = useState(false);
  const userId =
    row.kind === "user" ? row.principalUrn.replace(/^user:/, "") : null;
  const reaches = inheritedGrants(row).length > 0;
  const showFaces = row.kind === "role" && faces.length > 0;

  return (
    <div className="border-border col-span-full grid grid-cols-subgrid border-b last:border-b-0">
      <div className="col-span-full grid grid-cols-subgrid items-center gap-x-6 px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <PrincipalBadge kind={row.kind} />
          <span className="truncate font-medium">
            {userId ? (
              <IdentityLink identifier={{ userId }}>
                {row.displayName}
              </IdentityLink>
            ) : row.kind === "role" ? (
              <RoleLink principalUrn={row.principalUrn}>
                {row.displayName}
              </RoleLink>
            ) : (
              row.displayName
            )}
          </span>
        </div>

        <Text muted small className="min-w-0 truncate">
          {accessSummary(row, catalog)}
        </Text>

        {/* Who a role reaches, as the faces themselves: an administrator
            checks that before changing what the role can do, and "2 members"
            does not answer it. */}
        <div className="text-muted-foreground text-sm">
          {showFaces ? <MemberFacepile members={faces} maxFaces={5} /> : "—"}
        </div>

        <div className="flex items-center justify-end gap-1">
          <IconAction
            label={open ? "Done editing" : "Edit access"}
            onClick={() => setOpen((was) => !was)}
            expanded={open}
          >
            {/* A pencil says the row is editable, and the struck-through
                pencil says the editing is what stops. */}
            {open ? (
              <PencilOff className="h-4 w-4" />
            ) : (
              <Pencil className="h-4 w-4" />
            )}
          </IconAction>
          <IconAction
            // A role reached by an organization-wide rule is not removed
            // here, it is subtracted — so the label says what it does.
            label={
              canManage
                ? reaches
                  ? `Remove ${row.displayName} from this server`
                  : `Remove ${row.displayName}`
                : "Only organization admins can change access."
            }
            onClick={onRevoke}
            disabled={pending || !canManage}
          >
            <Trash2 className="h-4 w-4" />
          </IconAction>
        </div>
      </div>

      {/* Animated by rows rather than height, so the panel can size itself
          and still slide. Collapsed it is inert, so its controls are not
          reachable by keyboard or read out. */}
      <div
        className={cn(
          "col-span-full grid transition-[grid-template-rows] duration-200 ease-out motion-reduce:transition-none",
          open ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
        )}
        inert={!open}
        aria-hidden={!open}
      >
        <div className="overflow-hidden">
          <div className="border-border bg-muted/15 mx-4 mb-3 flex items-end justify-between gap-6 border p-4">
            <div className="space-y-3">
              {SCOPE_ROWS.map(({ key, label, hint }) => (
                <ScopeLine
                  key={key}
                  label={label}
                  hint={hint}
                  row={row}
                  scope={key}
                  catalog={catalog}
                  hasCatalog={hasCatalog}
                  onAllow={() => onAllow(key)}
                  onRevoke={() => onRevokeScope(key)}
                  onNarrow={onNarrow}
                  pending={pending}
                />
              ))}
            </div>
            {row.kind === "role" && (
              // The rule this row edits lives on the role; the link out sits
              // with the controls, out of the way of the lines being read.
              <Button
                variant="tertiary"
                size="sm"
                onClick={onEditRole}
                className="hover:bg-transparent h-auto shrink-0 px-0 py-0 font-sans normal-case tracking-normal underline decoration-dotted underline-offset-4 hover:decoration-solid"
              >
                <Button.Text className="font-sans normal-case tracking-normal">
                  Edit in Role Manager
                </Button.Text>
              </Button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function ScopeLine({
  label,
  hint,
  row,
  scope,
  catalog,
  hasCatalog,
  onAllow,
  onRevoke,
  onNarrow,
  pending,
}: {
  label: string;
  /** What this scope lets the principal do, in one line. */
  hint: string;
  row: AccessRow;
  scope: ScopeKey;
  /** The server's tools, for resolving what a rule and a block leave. */
  catalog: ToolSelectionTool[];
  hasCatalog: boolean;
  onAllow: () => void;
  onRevoke: () => void;
  onNarrow: () => void;
  pending: boolean;
}): JSX.Element {
  const state = scopeState(row, scope, catalog);
  const options = scopeOptions({
    row,
    scope,
    catalog,
    hasCatalog,
    onAllow,
    onRevoke,
    onNarrow,
  });

  return (
    <div className="flex items-baseline gap-2">
      <div className="w-44 shrink-0">
        <Text small>{label}</Text>
        <Text as="div" muted small className="text-xs">
          {hint}
        </Text>
      </div>
      {options.length === 0 ? (
        <Text small className="px-1">
          {state.value}
        </Text>
      ) : (
        <RequireScope
          scope="org:admin"
          level="component"
          reason="Only organization admins can change access."
        >
          <InlineChoice
            value={state.value}
            disabled={pending}
            options={options}
          />
        </RequireScope>
      )}
      {state.via && (
        <Text muted small>
          {state.granted ? "via " : "blocked by "}
          {state.viaPrincipalUrn?.startsWith("role:") ? (
            <RoleLink principalUrn={state.viaPrincipalUrn}>
              {state.via}
            </RoleLink>
          ) : (
            state.via
          )}
        </Text>
      )}
    </div>
  );
}

/**
 * What one scope line can be changed to. Every line an administrator can act
 * on offers a choice, whatever wrote the rule behind it: a grant covering
 * every server is edited here by subtracting from it for this server alone.
 */
function scopeOptions({
  row,
  scope,
  catalog,
  hasCatalog,
  onAllow,
  onRevoke,
  onNarrow,
}: {
  row: AccessRow;
  scope: ScopeKey;
  catalog: ToolSelectionTool[];
  hasCatalog: boolean;
  onAllow: () => void;
  onRevoke: () => void;
  onNarrow: () => void;
}): InlineChoiceOption[] {
  const state = scopeState(row, scope, catalog);
  // A block this row cannot lift closes the line, and nothing granted here
  // would survive it: the change has to be made where the block is.
  if (!state.granted && state.capped) return [];

  if (scope !== "use") {
    // View and manage cover the server itself; there is nothing inside one
    // to narrow, so the line is on or off.
    return state.granted
      ? [{ label: "No access", onSelect: onRevoke }]
      : [{ label: "Allowed", onSelect: onAllow }];
  }

  // Widening to every tool is only on offer when nothing above this line
  // caps it: a block on the role this person is in is not ours to lift.
  const options: InlineChoiceOption[] = state.capped
    ? []
    : [{ label: "All tools", onSelect: onAllow }];
  // Narrowing an organization-wide rule has to name the tools it takes away,
  // so a server that does not publish a catalogue cannot offer it.
  if (!state.subtracts || hasCatalog) {
    options.push({ label: "Specific tools\u2026", onSelect: onNarrow });
  }
  if (state.canRevoke) {
    options.push({ label: "No access", onSelect: onRevoke });
  }
  return options;
}

/** What kind of thing a row names, so a role does not read as a person. */
const PRINCIPAL_BADGE: Record<
  string,
  { label: string; variant: ComponentProps<typeof Badge>["variant"] }
> = {
  role: { label: "Role", variant: "warning" },
  user: { label: "Person", variant: "information" },
  everyone: { label: "Everyone", variant: "neutral" },
  directory_group: { label: "Group", variant: "neutral" },
  directory_attribute: { label: "Attribute", variant: "neutral" },
};

function PrincipalBadge({
  kind,
}: {
  kind: AccessRow["kind"];
}): JSX.Element | null {
  const badge = PRINCIPAL_BADGE[kind];
  if (!badge) return null;
  return (
    <Badge variant={badge.variant} size="sm">
      {badge.label}
    </Badge>
  );
}

/**
 * An icon-only row control. The icon carries the whole label, so the tooltip
 * opens without a delay — a reader hovering to find out what a button does
 * should not have to wait to be told.
 */
function IconAction({
  label,
  onClick,
  disabled,
  expanded,
  children,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  expanded?: boolean;
  children: JSX.Element;
}): JSX.Element {
  return (
    <Tooltip delayDuration={0}>
      <TooltipTrigger asChild>
        {/* A disabled button fires no pointer events, so the tooltip needs a
            wrapper to hover — which is exactly when the label matters most. */}
        <span>
          <Button
            variant="tertiary"
            size="sm"
            onClick={onClick}
            disabled={disabled}
            aria-expanded={expanded}
            aria-label={label}
          >
            <Button.LeftIcon>{children}</Button.LeftIcon>
          </Button>
        </span>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/**
 * A role name that goes to the role. The rule behind a line often lives on a
 * role, and the name is what a reader reaches for to go and change it.
 */
function RoleLink({
  principalUrn,
  children,
}: {
  principalUrn: string;
  children: ReactNode;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const roleId = principalUrn.split(":").pop();
  if (!roleId) return <>{children}</>;
  return (
    <Link
      to={`${orgRoutes.access.roles.href()}/${roleId}/edit`}
      className="underline decoration-dotted underline-offset-4 hover:decoration-solid"
      onClick={(event) => event.stopPropagation()}
    >
      {children}
    </Link>
  );
}
